# Launches a Testing-environment for Go
# Do NOT use to run for the demo
FROM golang:1.25.6

WORKDIR /app

COPY . .

CMD ["go", "run", "main.go", "localhost:4080"]