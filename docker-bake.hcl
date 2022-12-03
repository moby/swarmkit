variable "IMAGE_NAME" {
  default = "moby/swarmkit"
}

target "dev" {
  tags = [IMAGE_NAME]
  output = ["type=docker"]
}

group "default" {
  targets = ["validate"]
}

group "validate" {
  targets = ["lint", "vendor-validate", "generate-validate"]
}

group "lint" {
  targets = ["golangci-lint", "fmt-proto"]
}

target "golangci-lint" {
  target = "lint"
  output = ["type=cacheonly"]
}

target "fmt-proto" {
  target = "fmt-proto"
  output = ["type=cacheonly"]
}

target "vendor-validate" {
  target = "vendor-validate"
  output = ["type=cacheonly"]
}

target "vendor" {
  target = "vendor-update"
  output = ["."]
}

target "generate-validate" {
  target = "generate-validate"
  output = ["type=cacheonly"]
}

target "generate" {
  target = "generate-update"
  output = ["."]
}
