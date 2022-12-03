variable "IMAGE_NAME" {
  default = "moby/swarmkit"
}

target "dev" {
  tags = [IMAGE_NAME]
  output = ["type=docker"]
}
