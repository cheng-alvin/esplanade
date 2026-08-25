.PHONY: build run clean fmt vet

build:
	go build -o bin/esplanade-server .

run:
	ESPLANADE_ENV=development go run .

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin/
