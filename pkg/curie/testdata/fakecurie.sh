#!/bin/bash

# Function to handle the interrupt signal
cleanup() {
    echo "Received Interrupt signal, exiting..."
    exit 0
}

# Function to generate a random container ID
generate_container_id() {
    local chars=ABCDEFGHIJKLMNOPQRSTUVWXYZ
    local id="containerid-"
    for i in {1..6}; do
        id+="${chars:RANDOM%${#chars}:1}"
    done
    echo "$id"
}

# Directory to simulate container storage
FAKE_CURIE_DIR="$HOME/.fakecurie"
mkdir -p "$FAKE_CURIE_DIR"

# Handle different commands
case "$1" in
    "create")
        CONTAINER_ID=$(generate_container_id)
        echo "$CONTAINER_ID" > "$FAKE_CURIE_DIR/$CONTAINER_ID"
        echo "$CONTAINER_ID"
        exit 0
        ;;
    "start")
        if [[ -f "$FAKE_CURIE_DIR/$2" ]]; then
            trap cleanup EXIT
            trap cleanup SIGINT
            # Simulate a long-running process
            sleep 10000 &
            wait $!
            exit 0
        else
            echo "Container $2 does not exist."
            exit 1
        fi
        ;;
    "stop")
        if [[ -f "$FAKE_CURIE_DIR/$2" ]]; then
           echo "Container stopped"
           exit 0
        else
            echo "Container $2 does not exist."
            exit 1
        fi
        ;;
    "inspect")
        if [[ -f "$FAKE_CURIE_DIR/$2" ]]; then
            echo '{"arp":[{"IP":"192.168.1.10"}]}'
            exit 0
        else
            echo "Container $2 does not exist."
            exit 1
        fi
        ;;
    "rm")
        if [[ -f "$FAKE_CURIE_DIR/$2" ]]; then
            rm -f "$FAKE_CURIE_DIR/$2"
            echo "Removed $2"
            exit 0
        else
            echo "Container $2 does not exist."
            exit 1
        fi
        ;;
    *)
        echo "Invalid command."
        exit 1
        ;;
esac
