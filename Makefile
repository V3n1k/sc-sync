BINARY=sc-sync

install:
	go install ./...

update:
	go install github.com/V3n1k/sc-sync@latest

build:
	go build -o $(BINARY) .

restart:
	systemctl --user restart $(BINARY)

.PHONY: install update build restart
