GO = go

PREFIX = /usr/local
CFGPREFIX = /etc

EXE = dischord

all:
	$(GO) build -o $(EXE) cmd/$(EXE)/*.go

debug:
	$(GO) build -o $(EXE) -gcflags=all="-N -l" cmd/$(EXE)/*.go
	dlv exec ./dischord

fmt:
	find . -type f -name '*.go' -exec gofmt -w '{}' ';'

install: all
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	cp -f $(EXE) $(DESTDIR)$(PREFIX)/bin
	chmod 755 $(DESTDIR)$(PREFIX)/bin/$(EXE)
	mkdir -p $(DESTDIR)$(CFGPREFIX)/$(EXE)
	@if command -v systemd; then \
		$(MAKE) install-systemd; \
	else \
		@echo "Systemd not found, if you want to add $(DESTDIR)$(PREFIX)/bin/$(EXE) as a service, please do so manually."; \
	fi

install-systemd:
	mkdir -p $(DESTDIR)$(CFGPREFIX)/systemd/system
	echo \
[Unit] \
Description=$(EXE) \
Requires=network-online.target \
After=network-online.target \
[Service] \
Type=simple \
ExecStart=$(PREFIX)/bin/$(EXE) \
WorkingDirectory=$(CFGPREFIX)/$(EXE) \
Restart=on-failure \
[Install] \
WantedBy=multi-user.target \
| sed 's/ /\n/g' > $(DESTDIR)$(CFGPREFIX)/systemd/system/$(EXE).service

uninstall:
	@if command -v systemd; then \
		systemctl stop $(EXE); \
		systemctl disable $(EXE); \
	fi
	rm -f $(DESTDIR)$(PREFIX)/bin/$(EXE)
	rm -f $(DESTDIR)$(CFGPREFIX)/systemd/system/$(EXE).service

test:
	$(GO) test -count=1 -v $(EXE)/extractor

.PHONY: all debug fmt install uninstall clean

clean:
	rm -f $(EXE)
	rm -rf build/
