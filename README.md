# Rizzyscope

## Overview

**Rizzyscope** is a tool designed to interact with the Kismet wireless network detector. It allows users to monitor the RSSI (Received Signal Strength Indicator) of a specific MAC address on a specified network interface. The program leverages configuration files and command-line arguments for flexibility and ease of use.

## Features

- **Automatic Kismet launch:** Rizzyscope automatically launches Kismet, eliminating the need for manual startup.
- **MAC address monitoring:** The program searches for the specified MAC address via the Kismet API.
- **Channel locking:** Once the MAC address is found, Rizzyscope locks onto the channel of that device.
- **Real-time RSSI display:** Visualize the RSSI values in real-time within the terminal using a progress bar.
- **Configurable via TOML file:** Rizzyscope can be configured using a `config.toml` file for easy setup.
- **Command-line overrides:** Users can override configuration file settings directly via command-line arguments.

## Requirements

- **Kismet**: Rizzyscope requires Kismet to be installed on your machine. Kismet is a wireless network and device detector, sniffer, wardriving tool, and WIDS (Wireless Intrusion Detection) framework. Ensure that Kismet is installed and accessible in your system's PATH.
- **Go**: Ensure that Go is installed on your machine.
- **Root access**: The program needs to be run with root privileges to interact with network interfaces.

### Installing Kismet

To install Kismet, follow the instructions on the [official Kismet website](https://kismetwireless.net/).

## Installation

### Download the Latest Version

You can download the latest version of Rizzyscope from the [Releases](https://github.com/GobiasSomeCoffeeCo/rizzyscope/releases) page on GitHub. Download the binary for your operating system, extract it, and you’re ready to go.

### Building from Source

If you prefer, you can build Rizzyscope from source:

1. **Clone the repository**:
   ```bash
   git clone https://github.com/yourusername/rizzyscope.git
   cd rizzyscope
   ```
   
Build the program:

```bash

go build -o rizzyscope
```
## Usage
### Running the Program

Rizzyscope can be run with either a configuration file or command-line arguments. If both are provided, command-line arguments will override the configuration file settings.

#### Example 1: Using a Configuration File

Create a config.toml file in the same directory as the executable:

```toml

[required]
mac = "XX:XX:XX:XX:XX:XX"
interface = "wlp0s20f0u1u3"

[credentials]
user = "test"
password = "test"
```
#### Run the program:

```bash

sudo ./rizzyscope
```
#### Example 2: Overriding Config with Command-Line Arguments

You can override the mac and interface settings using command-line arguments:

```bash

sudo ./rizzyscope -m 12:34:ff:ee:ff -i wlp0s20f0u2u3
```
#### Example 3: Specifying a Different Config File

You can specify a different configuration file using the -c flag:

```bash

sudo ./rizzyscope -c /path/to/your/config.toml
```
Configuration

The program can be configured via a TOML file. The default configuration file is config.toml in the current directory.
Configuration File Structure

```toml
# All fields must be filled out. 

[required]
mac = "XX:XX:XX:XX:XX:XX"            # MAC address to monitor
interface = "wlp0s20f0u1u3"          # Network interface to use

[credentials]
user = "test"                        # Kismet username
password = "test"                    # Kismet password

```
## How It Works

- **Launch Kismet**: Rizzyscope automatically starts Kismet on the specified network interface.
- **Search for MAC Address**: The program queries the Kismet API to find the specified MAC address.
- **Lock to Channel**: Once the MAC address is detected, Rizzyscope locks the network interface to the appropriate channel.
- **Real-Time RSSI Display**: The RSSI for the MAC address is displayed in real-time using a terminal-based progress bar.


## Output

When running, the program will display a real-time progress bar in the terminal, representing the RSSI value of the specified MAC address.

For questions or issues, please open an issue on the GitHub repository.
