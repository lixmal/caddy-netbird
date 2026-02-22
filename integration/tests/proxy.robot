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
${CADDY_L4_TCP}      9191
${CADDY_L4_UDP}      9053
${CADDY_L4_SNI}      4443
${ECHO_SERVER_IP}    172.28.0.10
${ECHO_UDP_IP}       172.28.0.12
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
    ${body}=    Create Dictionary    node=ingress    address=${ECHO_SERVER_IP}:8080    network=tcp
    ${resp}=    POST    url=${PING_URL}    json=${body}    expected_status=200
    Should Be True    ${resp.json()}[reachable]    TCP ping should report reachable

Admin Ping UDP
    [Documentation]    Verify UDP ping through NetBird tunnel via admin API
    ${body}=    Create Dictionary    node=ingress    address=${ECHO_UDP_IP}:9053    network=udp
    ${resp}=    POST    url=${PING_URL}    json=${body}    expected_status=200
    Should Be True    ${resp.json()}[reachable]    UDP ping should report reachable

HTTP Reverse Proxy Through NB Tunnel
    [Documentation]    HTTP request through Caddy reverse proxy via NetBird tunnel
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify HTTP Proxy Response

HTTP Reverse Proxy Multiple Requests
    [Documentation]    Verify multiple sequential requests work through the tunnel
    FOR    ${i}    IN RANGE    5
        ${resp}=    GET    url=${CADDY_HTTP}    expected_status=200
        Should Contain    ${resp.text}    echo-ok
    END

L4 TCP Proxy Through NB Tunnel
    [Documentation]    Raw TCP echo through Caddy L4 proxy via NetBird tunnel
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify TCP Proxy Echo

L4 UDP Proxy Through NB Tunnel
    [Documentation]    UDP echo through Caddy L4 proxy via NetBird tunnel
    Wait Until Keyword Succeeds    30 sec    2 sec    Verify UDP Proxy Echo

L4 SNI TLS Passthrough
    [Documentation]    TLS passthrough via SNI routing through NetBird tunnel
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
    Should Contain    ${resp.text}    echo-ok

Verify TCP Proxy Echo
    ${result}=    Run Process    bash    -c
    ...    echo "hello-tcp" | nc -w 5 ${CADDY_L4_HOST} ${CADDY_L4_TCP}
    Should Be Equal As Integers    ${result.rc}    0    nc command failed
    Should Contain    ${result.stdout}    hello-tcp

Verify UDP Proxy Echo
    ${result}=    Run Process    bash    -c
    ...    echo "hello-udp" | nc -u -w 2 ${CADDY_L4_HOST} ${CADDY_L4_UDP}
    Should Contain    ${result.stdout}    hello-udp

Verify SNI TLS Passthrough
    ${result}=    Run Process    bash    -c
    ...    (echo "hello-sni"; sleep 1) | timeout 5 openssl s_client -connect ${CADDY_L4_HOST}:${CADDY_L4_SNI} -servername echo-tls.test -quiet 2>/dev/null
    Should Contain    ${result.stdout}    hello-sni
