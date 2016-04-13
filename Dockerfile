FROM alpine

ENV PATH /go/bin:/usr/local/go/bin:$PATH
ENV GOPATH /go

RUN	apk update && apk add \
	ca-certificates \
	&& rm -rf /var/cache/apk/*

COPY . /go/src/github.com/dcos/3dt

RUN buildDeps=' \
		go \
		git \
		gcc \
		libc-dev \
		libgcc \
	' \
	set -x \
	&& apk update \
	&& apk add $buildDeps \
	&& cd /go/src/github.com/dcos/3dt \
	&& go get -d -v github.com/dcos/3dt \
	&& go build -o /usr/bin/3dt . \
	&& apk del $buildDeps \
	&& rm -rf /var/cache/apk/* \
	&& rm -rf /go \
	&& echo "Build complete."


ENTRYPOINT [ "3dt" ]

