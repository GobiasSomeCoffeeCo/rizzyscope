# Rizzyscope

![](imgs/rizzyscope.jpg)

## Overview

Rizzyscope is a command-line tool designed for wireless network monitoring and analysis. Built as an interface to the Kismet wireless network detector, it provides real-time monitoring capabilities for network security professionals, researchers, and system administrators.

The tool enables users to monitor the Received Signal Strength Indicator (RSSI) of specific MAC addresses directly from the terminal, facilitating network troubleshooting, security auditing, and wireless device tracking. Rizzyscope streamlines the process of detecting unauthorized devices, monitoring network activity, and conducting wireless security assessments.

## Key Features

- **Automated Kismet Integration**: Automatically launches and configures Kismet, eliminating manual setup procedures
- **Target Device Monitoring**: Track specific devices by MAC address using the Kismet API
- **Channel Lock Functionality**: Automatically locks onto the target device's operating channel for focused monitoring
- **Real-Time RSSI Visualization**: Displays signal strength data through an intuitive terminal-based progress bar
- **Flexible Configuration**: Supports both TOML configuration files and command-line parameter overrides
- **Multi-Interface Support**: Compatible with multiple wireless network interfaces simultaneously

## System Requirements

### Dependencies

- **Kismet**: Wireless network detector and monitoring framework
  - Must be installed and accessible in system PATH
  - Visit [Kismet Official Website](https://kismetwireless.net/) for installation instructions
- **Go**: Go programming language runtime (for building from source)
- **Root Privileges**: Administrative access required for network interface manipulation

### Hardware Requirements

- Compatible wireless network interface capable of monitor mode
- Sufficient system resources for real-time signal processing

## Installation

### Binary Installation

Download the latest pre-compiled binary from the [GitHub Releases](https://github.com/GobiasSomeCoffeeCo/rizzyscope/releases) page. Select the appropriate version for your operating system and architecture.

### Building from Source

1. **Clone the repository**:

   ```bash
   git clone https://github.com/GobiasSomeCoffeeCo/rizzyscope.git
   cd rizzyscope
   ```

2. **Build the executable**:

   ```bash
   go build -o rizzyscope
   ```

## Configuration

Rizzyscope supports configuration through TOML files with command-line override capabilities.

### Configuration File Structure

Create a `config.toml` file in the executable directory:

```toml
[required]
target_mac = ["12:34:56:AA:CC:EE", "22:34:56:bb:cc:ee", "554456BBCCEE", "ab3423febc3d"]
interface = ["wlp0s20f0u2u3", "wlp0s20f0u2u4"]

[optional]
target_ssid = ["NetworkName1", "NetworkName2", "NetworkName3"]
kismet_endpoint = "127.0.0.1:2501"

[credentials]
user = "kismet_user"
password = "kismet_password"
```

### Configuration Parameters

- **target_mac**: Array of MAC addresses to monitor (required)
- **interface**: Network interfaces for monitoring (required)
- **target_ssid**: Filter by specific network names (optional)
- **kismet_endpoint**: Kismet server endpoint (default: 127.0.0.1:2501)
- **credentials**: Kismet authentication credentials

## Usage

### Basic Operation

**Using configuration file:**

```bash
sudo ./rizzyscope
```

**Command-line parameter override:**

```bash
sudo ./rizzyscope -m 11:34:56:23:23:EE,22:34:56:BB:BB:EE -i wlp0s20f0u2u3
```

**Custom configuration file:**

```bash
sudo ./rizzyscope -c /path/to/custom/config.toml
```

**Using existing Kismet instance:**

```bash
sudo ./rizzyscope --skip-kismet
# or
sudo ./rizzyscope -k
```

### Command-Line Options

- `-m, --mac`: Specify target MAC addresses (comma-separated)
- `-i, --interface`: Specify network interfaces (comma-separated)
- `-c, --config`: Path to configuration file
- `-k, --skip-kismet`: Use existing Kismet instance
- `--help`: Display usage information

## Operation Workflow

1. **Kismet Initialization**: Automatically launches Kismet on specified network interfaces
2. **Target Discovery**: Queries Kismet API to locate specified MAC addresses
3. **Channel Lock**: Locks network interface to target device's operating channel
4. **Real-Time Monitoring**: Continuously displays RSSI values through terminal interface

## Output Format

The application provides real-time RSSI monitoring through a terminal-based progress bar interface, displaying signal strength variations for tracked devices.

## Use Cases

- **Network Security Auditing**: Identify unauthorized devices and monitor network access
- **Wireless Troubleshooting**: Analyze signal strength and connectivity issues
- **Research and Development**: Support wireless network research and testing
- **Infrastructure Monitoring**: Track device presence and signal characteristics

## Support and Contributions

For technical support, bug reports, or feature requests, please submit an issue through the [GitHub Issues](https://github.com/GobiasSomeCoffeeCo/rizzyscope/issues) page.

## License

Please refer to the LICENSE file in the repository for licensing information.

## Disclaimer

This tool is intended for legitimate network monitoring and security research purposes. Users are responsible for ensuring compliance with applicable laws and regulations regarding wireless network monitoring in their jurisdiction.
