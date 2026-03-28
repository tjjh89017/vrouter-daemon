.PHONY: proto build clean

PROTO_DIR := proto
GEN_DIR := gen/go

proto:
	protoc \
		--go_out=$(GEN_DIR) --go_opt=module=github.com/tjjh89017/vrouter-daemon/gen/go \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=module=github.com/tjjh89017/vrouter-daemon/gen/go \
		-I$(PROTO_DIR) \
		$(PROTO_DIR)/control/v1/control.proto \
		$(PROTO_DIR)/agent/v1/agent.proto

build: proto
	go build -o bin/vrouter-server ./cmd/vrouter-server/
	go build -o bin/vrouter-agent ./cmd/vrouter-agent/

clean:
	rm -rf bin/ $(GEN_DIR)/controlpb $(GEN_DIR)/agentpb
