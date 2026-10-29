module github.com/teamgram/proto

go 1.27.0

// pluma: Go 1.27 and dependency versions raised to the root module's selection
// (govulncheck fixes in x/net, x/crypto, x/text, grpc).
toolchain go1.27.3

require (
	github.com/stretchr/testify v1.12.1
	golang.org/x/crypto v0.58.0
	golang.org/x/net v0.61.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)
