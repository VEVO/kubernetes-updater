export BINARY_NAME=roller
export BINARY_BUILD_COMMAND=sh -c "go build -v ."
export DC=docker-compose -f docker-compose-build.yaml
export TAG=$(shell git describe --tags)

clean:
	$(DC) down --rmi local --remove-orphans
	$(DC) rm -f
	if [ -a $(BINARY_NAME) ]; then rm $(BINARY_NAME); fi;

build:
	$(DC) run binary

release: build
	$(DC) run release

