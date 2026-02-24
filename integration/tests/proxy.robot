*** Settings ***
Library    RequestsLibrary
Library    Collections
Library    Process
Library    String

Suite Setup    Wait For NetBird Connection

*** Variables ***
${CADDY_ADMIN}       http://localhost:2019
${CADDY_HTTP}        http://localhost:8088
${CADDY_L4_HOST}     localhost
${CADDY_L4_TCP}      5432
${CADDY_L4_UDP}      9053
${CADDY_L4_SNI}      4443
${NGINX_IP}          172.28.0.10
${COREDNS_IP}        172.28.0.12
${STATUS_URL}        http://localhost:2019/netbird/status
${PING_URL}          http://localhost:2019/netbird/ping
${LOG_LEVEL_URL}     http://localhost:2019/netbird/log-level

*** Test Cases ***
Admin Status JSON Shows Connected Node
    [Documentation]    Verify the admin API reports a connected NetBird node with management and signal
    ${resp}=    GET    url=${STATUS_URL}    params=format=json    expected_status=200
    ${nodes}=    Get From Dictionary    ${resp.json()}    nodes
    Dictionary Should Contain Key    ${nodes}    ingress
    ${node}=    Get From Dictionary    ${nodes}    ingress
    ${mgmt}=    Get From Dictionary    ${node}    management
    Should Be True    ${mgmt}[connected]    Management should be connected
    ${signal}=    Get From Dictionary    ${node}    signal
    Should Be True    ${signal}[connected]    Signal should be connected
    ${local}=    Get From Dictionary    ${node}    local
    Should Not Be Empty    ${local}[ip]    Local IP should be set

Admin Status JSON Shows Routing Peer
    [Documentation]    Verify the routing peer appears in the peer list
    ${resp}=    GET    url=${STATUS_URL}    params=format=json    expected_status=200
    ${nodes}=    Get From Dictionary    ${resp.json()}    nodes
    ${node}=    Get From Dictionary    ${nodes}    ingress
    ${peers}=    Get From Dictionary    ${node}    peers
    ${peer_count}=    Get Length    ${peers}
    Should Be True    ${peer_count} >= 1    At least one peer should be visible

Admin Status Text Format
    [Documentation]    Verify the admin API returns human-readable text status
    ${resp}=    GET    url=${STATUS_URL}    expected_status=200
    Should Contain    ${resp.text}    ingress
    Should Contain    ${resp.text}    Management

Admin Set Log Level
    [Documentation]    Verify changing the NB log level at runtime
    ${body}=    Create Dictionary    level=debug
    ${resp}=    PUT    url=${LOG_LEVEL_URL}    json=${body}    expected_status=200
    # Restore to info
    ${body}=    Create Dictionary    level=info
    ${resp}=    PUT    url=${LOG_LEVEL_URL}    json=${body}    expected_status=200

Admin Ping TCP
    [Documentation]    Verify TCP ping through NetBird tunnel via admin API
    ${body}=    Create Dictionary    node=ingress    address=${NGINX_IP}:8080    network=tcp
    ${resp}=    POST    url=${PING_URL}    json=${body}    expected_status=200
    Should Be True    ${resp.json()}[reachable]    TCP ping should report reachable

Admin Ping UDP
    [Documentation]    Verify UDP ping through NetBird tunnel via admin API
    ${body}=    Create Dictionary    node=ingress    address=${COREDNS_IP}:53    network=udp
    ${resp}=    POST    url=${PING_URL}    json=${body}    expected_status=200
    Should Be True    ${resp.json()}[reachable]    UDP ping should report reachable

HTTP Reverse Proxy Returns Nginx JSON
    [Documentation]    HTTP request through Caddy reverse proxy via NetBird tunnel to nginx
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify HTTP Proxy Response

HTTP Reverse Proxy Health Endpoint
    [Documentation]    Verify the nginx /health endpoint through the tunnel
    ${resp}=    GET    url=${CADDY_HTTP}/health    expected_status=200
    Should Contain    ${resp.text}    healthy

HTTP Reverse Proxy Multiple Requests
    [Documentation]    Verify multiple sequential requests work through the tunnel
    FOR    ${i}    IN RANGE    5
        ${resp}=    GET    url=${CADDY_HTTP}    expected_status=200
        Should Contain    ${resp.text}    nginx
    END

L4 TCP Proxy PostgreSQL Query
    [Documentation]    PostgreSQL query through Caddy L4 TCP proxy via NetBird tunnel
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify PostgreSQL Connection

L4 TCP Proxy PostgreSQL Write And Read
    [Documentation]    Verify write and read through PostgreSQL via the tunnel
    ${result}=    Run Process    bash    -c
    ...    PGPASSWORD\=test psql -h ${CADDY_L4_HOST} -p ${CADDY_L4_TCP} -U test -d testdb -t -A -c "CREATE TABLE IF NOT EXISTS integration_test (id serial, name text); INSERT INTO integration_test (name) VALUES ('hello'); SELECT name FROM integration_test LIMIT 1;"
    Should Be Equal As Integers    ${result.rc}    0    psql write/read failed: ${result.stderr}
    Should Contain    ${result.stdout}    hello

L4 UDP Proxy DNS Query
    [Documentation]    DNS query through Caddy L4 UDP proxy via NetBird tunnel to CoreDNS
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify DNS Query App

L4 UDP Proxy DNS Query Second Record
    [Documentation]    Verify a second DNS record resolves through the tunnel
    ${result}=    Run Process    bash    -c
    ...    dig @${CADDY_L4_HOST} -p ${CADDY_L4_UDP} db.test.local A +short
    Should Be Equal As Integers    ${result.rc}    0    dig command failed: ${result.stderr}
    Should Contain    ${result.stdout}    10.0.0.2

L4 SNI TLS Passthrough
    [Documentation]    TLS passthrough via SNI routing through NetBird tunnel to nginx HTTPS
    Wait Until Keyword Succeeds    60 sec    5 sec    Verify SNI TLS Passthrough

*** Keywords ***
Wait For NetBird Connection
    [Documentation]    Wait until the NetBird client is connected to management and has peers
    Wait Until Keyword Succeeds    2 min    5 sec    NetBird Client Connected

NetBird Client Connected
    ${resp}=    GET    url=${STATUS_URL}    params=format=json    expected_status=200
    ${nodes}=    Get From Dictionary    ${resp.json()}    nodes
    Dictionary Should Contain Key    ${nodes}    ingress
    ${node}=    Get From Dictionary    ${nodes}    ingress
    ${mgmt}=    Get From Dictionary    ${node}    management
    Should Be True    ${mgmt}[connected]    Management not connected
    ${peers}=    Get From Dictionary    ${node}    peers
    ${peer_count}=    Get Length    ${peers}
    Should Be True    ${peer_count} >= 1    No peers connected yet

Verify HTTP Proxy Response
    ${resp}=    GET    url=${CADDY_HTTP}    expected_status=200
    Should Contain    ${resp.text}    nginx

Verify PostgreSQL Connection
    ${result}=    Run Process    bash    -c
    ...    PGPASSWORD\=test psql -h ${CADDY_L4_HOST} -p ${CADDY_L4_TCP} -U test -d testdb -t -A -c "SELECT 1"
    Should Be Equal As Integers    ${result.rc}    0    psql command failed: ${result.stderr}
    Should Contain    ${result.stdout}    1

Verify DNS Query App
    ${result}=    Run Process    bash    -c
    ...    dig @${CADDY_L4_HOST} -p ${CADDY_L4_UDP} app.test.local A +short
    Should Be Equal As Integers    ${result.rc}    0    dig command failed: ${result.stderr}
    Should Contain    ${result.stdout}    10.0.0.1

Verify SNI TLS Passthrough
    ${result}=    Run Process    bash    -c
    ...    curl -sk --resolve echo-tls.test:${CADDY_L4_SNI}:127.0.0.1 https://echo-tls.test:${CADDY_L4_SNI}/
    Should Be Equal As Integers    ${result.rc}    0    curl command failed: ${result.stderr}
    Should Contain    ${result.stdout}    nginx-tls
