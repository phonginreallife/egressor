'use client'

import { useState, useRef, useEffect } from 'react'
import { Send, Sparkles, Loader2 } from 'lucide-react'
import { motion, AnimatePresence } from 'framer-motion'

interface Message {
  role: 'user' | 'assistant'
  content: string
  timestamp: Date
}

const suggestedQuestions = [
  "Why did our egress cost triple yesterday?",
  "Which services changed behavior after the last deployment?",
  "Show me the top 5 unnecessary transfers",
  "What's causing the spike in cross-region traffic?",
  "How can we reduce our data transfer costs?",
]

export function ClaudeChat() {
  const [messages, setMessages] = useState<Message[]>([
    {
      role: 'assistant',
      content: "Hello! I'm FlowScope's AI assistant. I can help you understand your data transfer patterns, investigate anomalies, and find cost optimization opportunities. What would you like to know?",
      timestamp: new Date(),
    },
  ])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim() || isLoading) return

    const userMessage: Message = {
      role: 'user',
      content: input,
      timestamp: new Date(),
    }

    setMessages((prev) => [...prev, userMessage])
    setInput('')
    setIsLoading(true)

    try {
      const res = await fetch('/api/v1/intelligence/ask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question: input }),
      })

      let answer: string
      if (res.ok) {
        const data = await res.json()
        answer = data.answer || "I apologize, but I couldn't process that request. Please try again."
      } else {
        // Simulate response for demo
        answer = generateMockResponse(input)
      }

      const assistantMessage: Message = {
        role: 'assistant',
        content: answer,
        timestamp: new Date(),
      }

      setMessages((prev) => [...prev, assistantMessage])
    } catch (error) {
      const errorMessage: Message = {
        role: 'assistant',
        content: "I'm having trouble connecting to the analysis service. Please try again in a moment.",
        timestamp: new Date(),
      }
      setMessages((prev) => [...prev, errorMessage])
    } finally {
      setIsLoading(false)
    }
  }

  const handleSuggestedQuestion = (question: string) => {
    setInput(question)
  }

  return (
    <div className="flex flex-col h-[600px]">
      {/* Header */}
      <div className="flex items-center gap-3 pb-4 border-b border-flow-border">
        <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-purple-500 to-pink-500 flex items-center justify-center">
          <Sparkles className="w-5 h-5 text-white" />
        </div>
        <div>
          <h3 className="font-semibold text-flow-text">Ask Claude</h3>
          <p className="text-xs text-flow-muted">Powered by Claude for intelligent analysis</p>
        </div>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto py-4 space-y-4">
        <AnimatePresence>
          {messages.map((message, i) => (
            <motion.div
              key={i}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0 }}
              className={`flex ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}
            >
              <div
                className={`max-w-[80%] rounded-lg p-4 ${
                  message.role === 'user'
                    ? 'bg-flow-accent/20 border border-flow-accent/30 text-flow-text'
                    : 'bg-flow-surface border border-flow-border text-flow-text'
                }`}
              >
                <p className="text-sm whitespace-pre-wrap">{message.content}</p>
                <p className="text-xs text-flow-muted mt-2">
                  {message.timestamp.toLocaleTimeString()}
                </p>
              </div>
            </motion.div>
          ))}
        </AnimatePresence>

        {isLoading && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex justify-start"
          >
            <div className="bg-flow-surface border border-flow-border rounded-lg p-4">
              <div className="flex items-center gap-2 text-flow-muted">
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Analyzing...</span>
              </div>
            </div>
          </motion.div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Suggested Questions */}
      {messages.length === 1 && (
        <div className="py-4 border-t border-flow-border">
          <p className="text-xs text-flow-muted mb-3">Suggested questions:</p>
          <div className="flex flex-wrap gap-2">
            {suggestedQuestions.map((question, i) => (
              <button
                key={i}
                onClick={() => handleSuggestedQuestion(question)}
                className="text-xs px-3 py-1.5 rounded-full bg-flow-surface border border-flow-border text-flow-muted hover:text-flow-text hover:border-flow-accent/30 transition-colors"
              >
                {question}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Input */}
      <form onSubmit={handleSubmit} className="pt-4 border-t border-flow-border">
        <div className="flex gap-3">
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Ask about your data transfer patterns..."
            className="flex-1 bg-flow-bg border border-flow-border rounded-lg px-4 py-3 text-sm text-flow-text placeholder-flow-muted focus:outline-none focus:border-flow-accent/50 transition-colors"
            disabled={isLoading}
          />
          <button
            type="submit"
            disabled={!input.trim() || isLoading}
            className="px-4 py-3 rounded-lg bg-flow-accent text-white font-medium disabled:opacity-50 disabled:cursor-not-allowed hover:bg-flow-accent/80 transition-colors flex items-center gap-2"
          >
            <Send className="w-4 h-4" />
          </button>
        </div>
      </form>
    </div>
  )
}

function generateMockResponse(question: string): string {
  const lowerQuestion = question.toLowerCase()

  if (lowerQuestion.includes('egress') && lowerQuestion.includes('cost')) {
    return `Based on my analysis of your transfer data:

**Key Finding:** Your egress costs increased by 180% in the last 24 hours.

**Root Cause:**
- The \`order-service\` started sending significantly more data to external analytics endpoints
- A new feature deployment at 14:00 UTC added verbose logging to an external service

**Impact:**
- Estimated additional daily cost: $45.60
- Projected monthly impact: ~$1,368

**Recommendations:**
1. Review the logging configuration in order-service
2. Consider batching analytics data instead of streaming
3. Evaluate if all external data transfers are necessary`
  }

  if (lowerQuestion.includes('unnecessary') || lowerQuestion.includes('waste')) {
    return `I've identified the following potentially unnecessary transfers:

1. **prometheus → thanos** (Cross-AZ): 15GB/day
   - Metrics are being scraped every 15s but only 1m resolution is used
   - Potential savings: ~$4.50/day

2. **api-gateway → logging-service** (Internal): 28GB/day
   - Debug logs enabled in production
   - Potential savings: ~$0 (internal) but reduces load

3. **user-service → s3** (Egress): 8GB/day
   - Redundant backups to S3 (already done by infra team)
   - Potential savings: ~$0.72/day

**Total potential monthly savings: ~$156**`
  }

  if (lowerQuestion.includes('spike') || lowerQuestion.includes('increase')) {
    return `I've detected a traffic spike pattern:

**Timeline:**
- Started: 2 hours ago
- Peak: 45 minutes ago
- Current status: Declining

**Affected services:**
- \`order-service\` → \`payment-service\` (+340%)
- \`api-gateway\` → \`order-service\` (+220%)

**Likely cause:**
This correlates with a promotional campaign that started at the same time. The increase appears to be legitimate business traffic.

**Recommendation:**
No immediate action required, but consider pre-scaling for future promotions.`
  }

  return `I've analyzed your question about: "${question}"

Based on the current data:
- Total active services: 12
- Current egress rate: 2.3 GB/hour
- Active anomalies: 3

Would you like me to dive deeper into any specific aspect of your data transfer patterns?`
}
