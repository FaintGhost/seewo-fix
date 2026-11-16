.PHONY: help cli app test vet fmt

help:
	@echo 'make cli   Build seewo-fix/build/seewo-fix'
	@echo 'make app   Build the native app without a DMG'
	@echo 'make test  Run Go tests with the race detector'
	@echo 'make vet   Run Go static checks'
	@echo 'make fmt   Format Go source'

cli:
	mkdir -p seewo-fix/build
	cd seewo-fix && mise exec -- go build -o build/seewo-fix ./cmd/seewo-fix

app:
	cd seewo-fix && mise exec -- go tool mygo build -skip-dmg

test:
	cd seewo-fix && mise exec -- go test -race ./...

vet:
	cd seewo-fix && mise exec -- go vet ./...

fmt:
	cd seewo-fix && mise exec -- go fmt ./...
