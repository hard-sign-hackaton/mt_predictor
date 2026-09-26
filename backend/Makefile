# Backend build automation.
#
# Generated protobuf bindings are checked in, so `make build` and `make test`
# never require protoc. Only `make proto` needs the toolchain.

MODULE     := mt_predictor
PROTO_DIR  := ../ml/delay_service/proto
PROTO_NAME := delay_service.proto
GEN_DIR    := internal/mlclient/gen
BIN        := bin/mt-predictor-backend
FEEDER_BIN := bin/ndtpfeeder
DATASET    := ../../dataset/validate/traffic.csv

# `go install` drops the plugins into GOBIN, which is not always on PATH.
GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

.PHONY: all
all: build

.PHONY: build
build:
	go build -o $(BIN) .

.PHONY: run
run: build
	./$(BIN)

# Replays the decoded dataset onto the NDTP wire. The backend cannot tell it
# apart from the Docker emulator, so no second ingestion path is needed.
.PHONY: feeder
feeder:
	go build -o $(FEEDER_BIN) ./cmd/ndtpfeeder

.PHONY: feed
feed: feeder
	./$(FEEDER_BIN) -addr 127.0.0.1:9201 -file "$(DATASET)" -vehicles 11 -speed 60 -loop

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -rf bin

# Regenerate the Go gRPC client for the ML service from the Python service's
# contract. Requires: brew install protobuf
#                     go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#                     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
.PHONY: proto
proto: check-protoc
	mkdir -p $(GEN_DIR)
	protoc -I "$(PROTO_DIR)" \
		--go_out="$(GEN_DIR)"      --go_opt=paths=source_relative \
		--go_opt="M$(PROTO_NAME)=$(MODULE)/$(GEN_DIR);delayv1" \
		--go-grpc_out="$(GEN_DIR)" --go-grpc_opt=paths=source_relative \
		--go-grpc_opt="M$(PROTO_NAME)=$(MODULE)/$(GEN_DIR);delayv1" \
		$(PROTO_NAME)
	@echo "generated:" && ls -1 $(GEN_DIR)

.PHONY: check-protoc
check-protoc:
	@command -v protoc >/dev/null 2>&1 || { \
		echo "protoc not found. Install it with: brew install protobuf"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { \
		echo "protoc-gen-go not found. Install it with:"; \
		echo "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { \
		echo "protoc-gen-go-grpc not found. Install it with:"; \
		echo "  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"; exit 1; }
