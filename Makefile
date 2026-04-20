.PHONY: build dev test clean docker up down tidy

build:
	go build -o irc-service .

dev:
	go run .

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f irc-service
	rm -rf data

docker:
	docker build -t irc-service .

up:
	docker compose up -d --build

down:
	docker compose down
