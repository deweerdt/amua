sources:=$(shell find src -name '*.go')
all: bin/amua

bin/amua: $(sources)
	wgo install amua

test:
	wgo test -v amua/mime
