.PHONY: run test lint vet fmt

run:
	go run .

test:
	go test ./... -race -cover

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	gofmt -l -w .
