# FlowScope Intelligence Service (Python)
FROM python:3.11-slim AS builder

WORKDIR /app

RUN pip install --no-cache-dir hatch

COPY . .

RUN hatch build

# Runtime
FROM python:3.11-slim

WORKDIR /app

COPY --from=builder /app/dist/*.whl .
RUN pip install --no-cache-dir *.whl && rm *.whl

RUN useradd -r -u 1000 egressor
USER egressor

EXPOSE 8090

CMD ["egressor-intelligence"]
