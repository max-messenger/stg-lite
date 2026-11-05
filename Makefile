.PHONY: test_unit
test_unit:
	mkdir -p coverage
	go clean -testcache && go tool gotestsum -- -v -cover -coverprofile=coverage/unit.txt -race  ./internal/...

.PHONY: test_integration
test_integration:
	mkdir -p coverage
	go clean -testcache && go tool gotestsum -- -v -cover -coverprofile=coverage/integration.txt -race -run "TestIntegration" ./...

.PHONY: test_e2e
test_e2e:
	go clean -testcache && go tool gotestsum -- -v -run "TestE2E" ./...

.PHONY: test_cover
test_cover: test_unit
	go tool cover -func coverage/unit.txt | fgrep total | awk '{print $$1, $$3}'

lint:
	golangci-lint run --fix

.PHONY: dev_env_up dev_env_down
dev_env_up: dev_env_down
	docker-compose pull
	docker-compose up -d --build
dev_env_down:
	docker-compose down -v --remove-orphans

.PHONY: generate
generate:
	go generate -x ./...

migrate-new: ## Create migration
	go tool sql-migrate new -config=db/conf.yml -env=local $(name)
