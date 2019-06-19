export BINARY_NAME=roller
export DC=docker-compose -f docker-compose-build.yaml
export TAG=$(shell git describe --tags)

build: test
	$(DC) run binary

clean:
	$(DC) down --rmi local --remove-orphans
	$(DC) rm -f
	if [ -a $(BINARY_NAME) ]; then rm $(BINARY_NAME); fi;
	if [ -d pkg ]; then rm -rf pkg; fi;
	if [ -d src ]; then rm -rf src; fi;
	if [ -d vendor ]; then rm -rf vendor; fi;

test: godep
	$(DC) run test

release: build
	$(DC) run release

godep:
	$(DC) run godep
