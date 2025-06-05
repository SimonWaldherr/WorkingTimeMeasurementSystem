export GO111MODULE=on; export GOFLAGS="-buildvcs=false"; env GOOS=linux GOARCH=amd64 go build -ldflags "-w" .
