BINARY  := p2pool-go-vtc
GO      ?= go
VERSION ?= dev-g$(shell git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/CADMonkey21/p2pool-go-VTC/config.Version=$(VERSION)
IMAGE   ?= p2pool-go-vtc
FUZZTIME ?= 30s

# package:target pairs run by `make fuzz`
FUZZ := .:FuzzSplitPort \
	./stratum:FuzzHandleMinerConnection \
	./stratum:FuzzIsValidVtcAddress \
	./work:FuzzShareJSON \
	./work:FuzzShareChainWalk

.PHONY: all build test test-race vet fmt fuzz docker clean

all: fmt vet test build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

fuzz:
	@set -e; for f in $(FUZZ); do \
		pkg=$${f%%:*}; name=$${f##*:}; \
		echo "fuzz $$pkg $$name"; \
		$(GO) test -run '^$$' -fuzz "^$$name$$" -fuzztime $(FUZZTIME) -fuzzminimizetime 100x $$pkg; \
	done

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

clean:
	rm -f $(BINARY)
