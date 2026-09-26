BINARY := arabic-tts
PHRASE := السلام عليكم ورحمة الله وبركاته، هذا اختبار للنظام

.PHONY: build test vet fmt lint clean doctor roundtrip integration completions

build:
	go build -o $(BINARY) ./cmd/arabic-tts

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint: vet
	gofmt -l .

integration:
	ARABIC_TTS_INTEGRATION=1 go test -tags integration -run Live -v ./internal/cli/

doctor: build
	./$(BINARY) doctor

roundtrip: build
	@mkdir -p out
	./$(BINARY) tts "$(PHRASE)" -o out/roundtrip.wav
	./$(BINARY) stt out/roundtrip.wav

clean:
	rm -f $(BINARY)
	rm -rf out

COMPDIR := $(HOME)/.local/share/zsh/site-functions

completions: build
	@mkdir -p $(COMPDIR)
	./$(BINARY) completion zsh > $(COMPDIR)/_$(BINARY)
	@rm -f $${ZDOTDIR:-$$HOME}/.zcompdump
	@echo "installed $(COMPDIR)/_$(BINARY) — restart zsh to pick it up"
