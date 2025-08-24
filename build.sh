export GO111MODULE=on; export GOFLAGS="-buildvcs=false"; 
env GOOS=linux GOARCH=amd64 go build -ldflags "-w" -o .bin/WorkingTime-linux-amd64 . ;
env GOOS=darwin GOARCH=arm64 go build -ldflags "-w" -o .bin/WorkingTime-darwin-arm64 . ;
