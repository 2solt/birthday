.PHONY: db-up-kind

dev:
	docker compose up

# Simple way to run postgres in Kind's network
db-up-kind:
	docker compose -f docker-compose.yaml -f docker-compose-kind.yaml up postgres --detach --yes

