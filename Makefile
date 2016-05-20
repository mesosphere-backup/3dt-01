PLATFORM := $(shell uname)
REV := $(shell git rev-parse --short HEAD)
LDFLAGS := -X github.com/dcos/3dt/api.Revision=$(REV)

all: test install

test:
	@echo "+$@"
	go get github.com/stretchr/testify
	go get -u github.com/golang/lint/golint
	golint -set_exit_status .
	golint -set_exit_status api
	go test -race -cover -test.v $(shell go list ./... | grep -v /vendor/)

build:
	@echo "+$@"
	go build -v -ldflags '$(LDFLAGS)'

install:
	@echo "+$@"
	go install -v -ldflags '$(LDFLAGS)'

clean:
	rm -f ./3dt
