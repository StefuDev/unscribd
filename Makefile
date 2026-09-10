.PHONY: build test pgo-build

# A profile is an optional optimization artifact, not a source dependency.
PGO_FLAGS := $(if $(wildcard default.pgo),-pgo=default.pgo,)

build:
	go build -buildvcs=false -trimpath $(PGO_FLAGS) -ldflags="-s -w" -o bin/unscribd ./cmd/unscribd
	go build -buildvcs=false -trimpath $(PGO_FLAGS) -ldflags="-s -w" -o bin/unscribd-web ./cmd/unscribd-web

test:
	go test ./...
	go vet ./...

# Generate default.pgo first with:
#   bin/unscribd -cpuprofile=default.pgo <representative Scribd URL>
pgo-build:
	test -f default.pgo
	go build -buildvcs=false -trimpath -pgo=default.pgo -ldflags="-s -w" -o bin/unscribd ./cmd/unscribd
	go build -buildvcs=false -trimpath -pgo=default.pgo -ldflags="-s -w" -o bin/unscribd-web ./cmd/unscribd-web
