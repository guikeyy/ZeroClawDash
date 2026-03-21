.PHONY: build clean build-armv7 build-all

BINARY_NAME=zeroclawdash
VERSION=1.2.0

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY_NAME) main.go

build-armv7:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY_NAME)-armv7 main.go

build-all: build build-armv7

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-armv7

test:
	go test -v ./...

run:
	go run main.go
