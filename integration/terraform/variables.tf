variable "backend_subnet" {
  description = "CIDR of the Docker backend network where echo servers run"
  type        = string
  default     = "172.28.0.0/24"
}
