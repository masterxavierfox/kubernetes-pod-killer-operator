VERSION=v1.0.0
PATH_BUILD=dist/
FILE_COMMAND=butcherctl
FILE_ARCH=linux

clean:
	@rm -rf ./dist

commit:
#	git pull
#	git pull --recurse-submodules origin master
#	git add .; git commit -am "Kratos power";git push
#	git tag -a $(VERSION) -m "Kratos Power"
#	git push origin $(VERSION)
	git add -A && git commit -m "$(curl -s whatthecommit.com/index.txt)" && git push


build: clean commit local

deploy:
	goreleaser --rm-dist

version:
	@echo $(VERSION)

install:
	install -d -m 755 '$(HOME)/bin/'
	install $(PATH_BUILD)$(VERSION)/$(FILE_ARCH)/$(FILE_COMMAND) '$(HOME)/bin/$(FILE_COMMAND)'

setup:
#	git config --global url."git@github.com:".insteadOf "https://github.com/"
#	go get -d -v git.prod.cellulant.com/ops-templates/ci-cd-tools/devops-kratos
#	cd $HOME/go/src/git.prod.cellulant.com/ops-templates/ci-cd-tools/devops-kratos
#	git pull --recurse-submodules origin master
#	dep init
#	dep ensure -vendor-only
	go mod tidy


local:
	#export GO111MODULE=off
	rm -rf build
	go build -o build/$(FILE_COMMAND) main.go
#	sudo install ./$(FILE_COMMAND) '/usr/local/bin/$(FILE_COMMAND)'

run: local
	./build/$(FILE_COMMAND)