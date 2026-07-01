.PHONY: build test clean studio serve

# Same binary, two names: `kram` runs scripts (kram file.kr), `krapl` opens the
# REPL. Behaviour is chosen at runtime from the invoked name.
build:
	go build -o kram .
	go build -o krapl .

test:
	go test ./...

# studio: compile the interpreter to WASM and drop the Go runtime shim next to
# the page. wasm.go is the browser entry (built only under GOOS=js).
studio:
	GOOS=js GOARCH=wasm go build -o studio/kram.wasm .
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" studio/wasm_exec.js

# serve: build then host studio/ locally (WASM needs http, not file://).
serve: studio
	@echo "kraM Studio → http://localhost:8080"
	@cd studio && python3 -m http.server 8080

clean:
	rm -f kram krapl studio/kram.wasm studio/wasm_exec.js
