sources:=$(shell find src -name '*.go')
all: bin/amua

bin/amua: $(sources)
	wgo restore
	wgo install amua

test:
	wgo restore
	wgo test -v amua/mime

.PHONY: tags
tags:
	gotags -R src > tags
	gotags -R vendor/src >> tags
