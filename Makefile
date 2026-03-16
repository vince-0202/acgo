.PHONY: build run clean

# Build the acgo binary into dist/
build:
	@mkdir -p dist
	go build -o dist/acgo ./cmd/acgo

# Build and run. Usage: make run [ARGS="chat"] or make run ARGS="print hello"
run: build
	./dist/acgo $(if $(ARGS),$(ARGS),chat)

# Remove build output
clean:
	rm -rf dist
