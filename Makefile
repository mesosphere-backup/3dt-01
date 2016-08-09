all: test install

test:
	@echo "+$@"
	go test -race -cover -test.v $(shell go list ./... | grep -v /vendor/)

build:
	@echo "+$@"
	go build -v

install:
	@echo "+$@"
	go install -v

clean:
	rm -f ./3dt
