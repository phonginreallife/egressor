// Package agent implements the FlowScope node agent.
package agent

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/egressor/egressor/src/pkg/types"
)

// K8sEnricher enriches events with Kubernetes metadata.
type K8sEnricher struct {
	client   kubernetes.Interface
	ipToPod  map[string]*PodInfo
	mu       sync.RWMutex
	stopChan chan struct{}
}

// PodInfo holds pod metadata.
type PodInfo struct {
	Name      string
	Namespace string
	NodeName  string
	Labels    map[string]string
	OwnerKind string
	OwnerName string
}

// NewK8sEnricher creates a new Kubernetes enricher.
func NewK8sEnricher() (*K8sEnricher, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get in-cluster config, K8s enrichment disabled")
		return &K8sEnricher{
			ipToPod:  make(map[string]*PodInfo),
			stopChan: make(chan struct{}),
		}, nil
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	e := &K8sEnricher{
		client:   client,
		ipToPod:  make(map[string]*PodInfo),
		stopChan: make(chan struct{}),
	}

	// Start watching pods
	go e.watchPods()

	return e, nil
}

// GetIdentity returns service identity for an IP.
func (e *K8sEnricher) GetIdentity(ip string) *types.ServiceIdentity {
	e.mu.RLock()
	defer e.mu.RUnlock()

	pod, ok := e.ipToPod[ip]
	if !ok {
		return nil
	}

	return &types.ServiceIdentity{
		Namespace: pod.Namespace,
		Name:      pod.OwnerName,
		Kind:      pod.OwnerKind,
		PodName:   pod.Name,
		NodeName:  pod.NodeName,
		Labels:    pod.Labels,
		Team:      pod.Labels["team"],
		Version:   pod.Labels["version"],
	}
}

// watchPods watches for pod changes.
func (e *K8sEnricher) watchPods() {
	if e.client == nil {
		return
	}

	for {
		select {
		case <-e.stopChan:
			return
		default:
		}

		watcher, err := e.client.CoreV1().Pods("").Watch(context.Background(), metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Msg("Failed to watch pods")
			time.Sleep(5 * time.Second)
			continue
		}

		e.processWatchEvents(watcher)
	}
}

// processWatchEvents processes pod watch events.
func (e *K8sEnricher) processWatchEvents(watcher watch.Interface) {
	defer watcher.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}

			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				e.addPod(pod)
			case watch.Deleted:
				e.removePod(pod)
			}
		}
	}
}

// addPod adds or updates a pod in the cache.
func (e *K8sEnricher) addPod(pod *corev1.Pod) {
	if pod.Status.PodIP == "" {
		return
	}

	// Get owner reference
	ownerKind := "Pod"
	ownerName := pod.Name
	for _, ref := range pod.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			ownerKind = ref.Kind
			ownerName = ref.Name
			// If owned by ReplicaSet, try to get Deployment name
			if ref.Kind == "ReplicaSet" {
				// ReplicaSet names are usually <deployment>-<hash>
				// Strip the hash suffix
				ownerName = stripReplicaSetSuffix(ref.Name)
				ownerKind = "Deployment"
			}
			break
		}
	}

	info := &PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		NodeName:  pod.Spec.NodeName,
		Labels:    pod.Labels,
		OwnerKind: ownerKind,
		OwnerName: ownerName,
	}

	e.mu.Lock()
	e.ipToPod[pod.Status.PodIP] = info
	// Also index by pod IPs in status
	for _, podIP := range pod.Status.PodIPs {
		e.ipToPod[podIP.IP] = info
	}
	e.mu.Unlock()

	log.Debug().
		Str("pod", pod.Name).
		Str("namespace", pod.Namespace).
		Str("ip", pod.Status.PodIP).
		Str("owner", ownerName).
		Msg("Pod added to cache")
}

// removePod removes a pod from the cache.
func (e *K8sEnricher) removePod(pod *corev1.Pod) {
	e.mu.Lock()
	delete(e.ipToPod, pod.Status.PodIP)
	for _, podIP := range pod.Status.PodIPs {
		delete(e.ipToPod, podIP.IP)
	}
	e.mu.Unlock()
}

// stripReplicaSetSuffix strips the hash suffix from ReplicaSet name.
func stripReplicaSetSuffix(name string) string {
	// ReplicaSet names are <deployment>-<hash> where hash is 10 chars
	if len(name) > 11 {
		// Check if last segment looks like a hash
		lastDash := -1
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '-' {
				lastDash = i
				break
			}
		}
		if lastDash > 0 && len(name)-lastDash-1 >= 5 {
			return name[:lastDash]
		}
	}
	return name
}

// Stop stops the enricher.
func (e *K8sEnricher) Stop() {
	close(e.stopChan)
}

// GetPodCount returns number of cached pods.
func (e *K8sEnricher) GetPodCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.ipToPod)
}

// ServiceEnricher provides service-level enrichment.
type ServiceEnricher struct {
	client        kubernetes.Interface
	serviceToIPs  map[string][]string
	ipToService   map[string]string
	mu            sync.RWMutex
}

// NewServiceEnricher creates a service enricher.
func NewServiceEnricher(client kubernetes.Interface) *ServiceEnricher {
	e := &ServiceEnricher{
		client:       client,
		serviceToIPs: make(map[string][]string),
		ipToService:  make(map[string]string),
	}

	if client != nil {
		go e.watchServices()
		go e.watchEndpoints()
	}

	return e
}

// GetServiceName returns service name for an IP.
func (e *ServiceEnricher) GetServiceName(ip string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ipToService[ip]
}

// watchServices watches for service changes.
func (e *ServiceEnricher) watchServices() {
	for {
		watcher, err := e.client.CoreV1().Services("").Watch(context.Background(), metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Msg("Failed to watch services")
			time.Sleep(5 * time.Second)
			continue
		}

		for event := range watcher.ResultChan() {
			svc, ok := event.Object.(*corev1.Service)
			if !ok {
				continue
			}

			key := svc.Namespace + "/" + svc.Name
			e.mu.Lock()
			if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != "None" {
				e.ipToService[svc.Spec.ClusterIP] = key
			}
			e.mu.Unlock()
		}
	}
}

// watchEndpoints watches for endpoint changes.
func (e *ServiceEnricher) watchEndpoints() {
	for {
		watcher, err := e.client.CoreV1().Endpoints("").Watch(context.Background(), metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Msg("Failed to watch endpoints")
			time.Sleep(5 * time.Second)
			continue
		}

		for event := range watcher.ResultChan() {
			ep, ok := event.Object.(*corev1.Endpoints)
			if !ok {
				continue
			}

			key := ep.Namespace + "/" + ep.Name
			var ips []string

			for _, subset := range ep.Subsets {
				for _, addr := range subset.Addresses {
					ips = append(ips, addr.IP)
				}
			}

			e.mu.Lock()
			// Remove old IPs
			if oldIPs, ok := e.serviceToIPs[key]; ok {
				for _, ip := range oldIPs {
					delete(e.ipToService, ip)
				}
			}
			// Add new IPs
			e.serviceToIPs[key] = ips
			for _, ip := range ips {
				e.ipToService[ip] = key
			}
			e.mu.Unlock()
		}
	}
}
