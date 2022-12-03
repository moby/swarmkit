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
  targets = ["vendor-validate", "generate-validate"]
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
