resource "netbird_group" "caddy_peers" {
  name = "caddy-peers"
}

resource "netbird_group" "routing_peers" {
  name = "routing-peers"
}

resource "netbird_group" "network_resources" {
  name = "network-resources"
}

resource "netbird_setup_key" "caddy" {
  name           = "caddy-integration"
  type           = "reusable"
  expiry_seconds = 86400
  auto_groups    = [netbird_group.caddy_peers.id]
}

resource "netbird_setup_key" "routing_peer" {
  name           = "routing-peer-integration"
  type           = "reusable"
  expiry_seconds = 86400
  auto_groups    = [netbird_group.routing_peers.id]
}

resource "netbird_policy" "caddy_to_resources" {
  name        = "caddy-to-resources"
  description = "Allow caddy peers to reach network resources via routing peers"
  enabled     = true

  rule {
    name          = "caddy-to-resources"
    enabled       = true
    action        = "accept"
    bidirectional = false
    protocol      = "all"
    sources       = [netbird_group.caddy_peers.id]
    destinations  = [netbird_group.network_resources.id]
  }
}

resource "netbird_network" "backend" {
  name        = "backend-network"
  description = "Docker backend network for echo servers"
}

resource "netbird_network_router" "backend_router" {
  network_id  = netbird_network.backend.id
  peer_groups = [netbird_group.routing_peers.id]
  masquerade  = true
  metric      = 100
}

resource "netbird_network_resource" "backend_subnet" {
  network_id = netbird_network.backend.id
  name       = "backend-subnet"
  address    = var.backend_subnet
  groups     = [netbird_group.network_resources.id]
}
