output "caddy_setup_key" {
  value     = netbird_setup_key.caddy.key
  sensitive = true
}

output "routing_peer_setup_key" {
  value     = netbird_setup_key.routing_peer.key
  sensitive = true
}

output "backend_subnet" {
  value = var.backend_subnet
}
