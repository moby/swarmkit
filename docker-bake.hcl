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
  targets = ["generate-validate"]
}

target "generate-validate" {
  target = "generate-validate"
  output = ["type=cacheonly"]
}

target "generate" {
  target = "generate-update"
  output = ["."]
}
