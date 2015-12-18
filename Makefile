NAME=queryable
VERSION=$(shell cat VERSION)

dev:
	@docker history $(NAME):dev &> /dev/null \
		|| docker build -f Dockerfile.dev -t $(NAME):dev .
	@docker run --rm \
		--net=host \
		-p 0.0.0.0:53:53/udp \
		-v $(PWD):/go/src/github.com/ekristen/queryable \
		-v /var/run/docker.sock:/tmp/docker.sock \
		$(NAME):dev

build:
	mkdir -p build
	git rev-parse --short HEAD > VERSION_BUILD
	docker build -t $(NAME):$(VERSION) .
	docker save $(NAME):$(VERSION) | gzip -9 > build/$(NAME)_$(VERSION).tgz

test:
	GOMAXPROCS=4 go test -v ./... -race

release:
	rm -rf release && mkdir release
	go get github.com/progrium/gh-release/...
	cp build/* release
	gh-release create ekristen/$(NAME) $(VERSION) \
		$(shell git rev-parse --abbrev-ref HEAD) $(VERSION)

.PHONY: build release
