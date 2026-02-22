terraform {
  required_version = ">= 1.0"

  required_providers {
    netbird = {
      source  = "netbirdio/netbird"
      version = "~> 0.0.4"
    }
  }
}

provider "netbird" {
  # Configured via environment variables:
  # NB_MANAGEMENT_URL - management server URL
  # NB_PAT            - personal access token
}
