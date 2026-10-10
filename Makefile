.PHONY: run build ui typecheck clean

# Everything: build the binary (UI must already be embedded).
build:
	go build -o tally-web-ui.exe .

run:
	go run .

# Rebuild the Vue app and re-embed it. Needs Node 20+.
ui:
	npm --prefix frontend install
	npm --prefix frontend run build
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	cp -r frontend/dist/. internal/web/dist/
	go build -o tally-web-ui.exe .

typecheck:
	go vet ./...
	npm --prefix frontend run typecheck

clean:
	rm -f tally-web-ui tally-web-ui.exe
	rm -rf data
