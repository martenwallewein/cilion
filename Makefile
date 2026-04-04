.PHONY: install

install:
	apt-get install -y clang llvm libbpf-dev linux-tools-$(shell uname -r)
