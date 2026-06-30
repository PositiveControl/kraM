.PHONY: build test clean

# Same binary, two names: `kram` runs scripts (kram file.kr), `krapl` opens the
# REPL. Behaviour is chosen at runtime from the invoked name.
build:
	go build -o kram .
	go build -o krapl .

test:
	go test ./...

clean:
	rm -f kram krapl
