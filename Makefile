APP_NAME := s3browser
BUILD_DIR := build

.PHONY: build run clean

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) .

run:
	go run .

clean:
	rm -rf $(BUILD_DIR)
