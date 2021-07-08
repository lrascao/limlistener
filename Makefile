
all: build

build:
	rm -f ./listener-tester
	go build -o listener-tester ./example

run: build
	./listener-tester

verify:
	go mod tidy
	go mod download
	go vet ./...
	go fmt ./...
	golangci-lint run

