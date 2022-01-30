.PHONY: client
client:
	go build -o bin/client client.go

.PHONY: server
server:
	go build -o bin/server server/server.go

.PHONY: tracing
tracing:
	go build -o bin/tracing tracing-server/main.go

.PHONY: all
all: client tracing server


.PHONY: clean
clean:
	rm -rf bin/*
