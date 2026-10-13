# Override with `make DOCKER=docker ...` when your user is in the docker group.
DOCKER ?= sudo docker
ADMIN_USER ?= admin2

.DEFAULT_GOAL := help
.PHONY: help image up down logs reset admin

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

image: ## Build the knoxmgr and knoxmgr-mock images from the repo root
	$(MAKE) -C .. image

up: ## Start knoxmock and knoxmgr on http://localhost:8088
	$(DOCKER) compose up -d

down: ## Stop the stack, keep data
	$(DOCKER) compose down

logs: ## Follow logs
	$(DOCKER) compose logs -f

reset: ## Stop the stack and delete the data volume
	$(DOCKER) compose down -v

admin: ## Create another local admin: make admin ADMIN_USER=name (prompts for password)
	$(DOCKER) compose exec -it knoxmgr /usr/local/bin/knoxmgr -config /etc/knoxmgr/config.yaml -create-admin $(ADMIN_USER)
