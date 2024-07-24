/*
 * foundationdb_process_address.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1beta2

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ProcessAddress provides a structured address for a process.
type ProcessAddress struct {
	IPAddress     net.IP          `json:"address,omitempty"`
	StringAddress string          `json:"stringAddress,omitempty"`
	Port          int             `json:"port,omitempty"`
	Flags         map[string]bool `json:"flags,omitempty"`
	FromHostname  bool            `json:"fromHostname,omitempty"`
}

// NewProcessAddress creates a new ProcessAddress if the provided string address is a valid IP address it will be set as
// IPAddress.
func NewProcessAddress(address net.IP, stringAddress string, port int, flags map[string]bool) ProcessAddress {
	pAddr := ProcessAddress{
		IPAddress:     address,
		StringAddress: stringAddress,
		Port:          port,
		Flags:         flags,
	}

	// If we have a valid IP address in the Placeholder we can set the
	// IPAddress accordingly.
	ip := net.ParseIP(pAddr.StringAddress)
	if ip != nil {
		pAddr.IPAddress = ip
		pAddr.StringAddress = ""
	}

	return pAddr
}

// IsEmpty returns true if a ProcessAddress is not set
func (address ProcessAddress) IsEmpty() bool {
	return address.IPAddress == nil
}

// Equal checks if two ProcessAddress are the same
func (address ProcessAddress) Equal(addressB ProcessAddress) bool {
	if !address.IPAddress.Equal(addressB.IPAddress) {
		return false
	}

	if address.Port != addressB.Port {
		return false
	}

	if len(address.Flags) != len(addressB.Flags) {
		return false
	}

	for k, v := range address.Flags {
		if v != addressB.Flags[k] {
			return false
		}
	}

	return true
}

// SortedFlags returns a list of flags on an address, sorted lexographically.
func (address ProcessAddress) SortedFlags() []string {
	flags := make([]string, 0, len(address.Flags))
	for flag, set := range address.Flags {
		if set {
			flags = append(flags, flag)
		}
	}

	sort.Slice(flags, func(i int, j int) bool {
		return flags[i] < flags[j]
	})

	return flags
}

// ProcessAddressesString converts a slice of ProcessAddress into a string joined by the separator
func ProcessAddressesString(pAddrs []ProcessAddress, sep string) string {
	sb := strings.Builder{}
	maxIdx := len(pAddrs) - 1
	for idx, pAddr := range pAddrs {
		sb.WriteString(pAddr.String())

		if idx < maxIdx {
			sb.WriteString(sep)
		}
	}

	return sb.String()
}

// ProcessAddressesStringWithoutFlags converts a slice of ProcessAddress into a string joined by the separator
// without the flags
func ProcessAddressesStringWithoutFlags(pAddrs []ProcessAddress, sep string) string {
	sb := strings.Builder{}
	maxIdx := len(pAddrs) - 1
	for idx, pAddr := range pAddrs {
		sb.WriteString(pAddr.StringWithoutFlags())

		if idx < maxIdx {
			sb.WriteString(sep)
		}
	}

	return sb.String()
}

// UnmarshalJSON defines the parsing method for the ProcessAddress field from JSON to struct
func (address *ProcessAddress) UnmarshalJSON(data []byte) error {
	trimmed := strings.Trim(string(data), "\"")
	parsedAddr, err := ParseProcessAddress(trimmed)
	if err != nil {
		return err
	}

	*address = parsedAddr

	return nil
}

// MarshalJSON defines the parsing method for the ProcessAddress field from struct to JSON
func (address ProcessAddress) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", address.String())), nil
}

// ParseProcessAddress parses a structured address from its string
// representation.
func ParseProcessAddress(address string) (ProcessAddress, error) {
	result := ProcessAddress{}
	if strings.HasSuffix(address, "(fromHostname)") {
		address = strings.TrimSuffix(address, "(fromHostname)")
		result.FromHostname = true
	}

	// For all Pod based actions we only provide an IP address without the port to actually
	// like exclusions and includes. If the address is a valid IP address we can directly skip
	// here and return the Process address, since the address doesn't contain any ports or additional flags.
	ip := net.ParseIP(address)
	if ip != nil {
		result.IPAddress = ip
		return result, nil
	}

	// In order to find the address port pair we will go over the address stored in a tmp String.
	// The idea is to split from the right to the left. If we find a Substring that is not a valid host port pair
	// we can trim the last part and store it as a flag e.g. ":tls" and try the next substring with the flag removed.
	// Currently FoundationDB only supports the "tls" flag but with this function we are able to also parse any additional
	// future flags.
	tmpStr := address
	for tmpStr != "" {
		addr, port, err := net.SplitHostPort(tmpStr)

		// If we didn't get an error we found the addr:port
		// part of the ProcessAddress.
		if err == nil {
			result.IPAddress = net.ParseIP(addr)
			if result.IPAddress == nil {
				result.StringAddress = addr
			}
			iPort, err := strconv.Atoi(port)
			if err != nil {
				return result, err
			}
			result.Port = iPort
			break
		}

		// Search for the last : in our tmpStr.
		// If there is no other : in our tmpStr we can abort since we don't
		// have a valid address port pair left.
		idx := strings.LastIndex(tmpStr, ":")
		if idx == -1 {
			break
		}

		if result.Flags == nil {
			result.Flags = map[string]bool{}
		}

		result.Flags[tmpStr[idx+1:]] = true
		tmpStr = tmpStr[:idx]
	}

	return result, nil
}

func parseAddresses(addrs []string) ([]ProcessAddress, error) {
	pAddresses := make([]ProcessAddress, len(addrs))

	for idx, addr := range addrs {
		pAddr, err := ParseProcessAddress(addr)
		if err != nil {
			return pAddresses, err
		}

		pAddresses[idx] = pAddr
	}

	return pAddresses, nil
}

// ParseProcessAddressesFromCmdline returns the ProcessAddress slice parsed from the commandline
// of the process.
func ParseProcessAddressesFromCmdline(cmdline string) ([]ProcessAddress, error) {
	addrReg, err := regexp.Compile(`--public_address=(\S+)`)
	if err != nil {
		return nil, err
	}

	res := addrReg.FindStringSubmatch(cmdline)
	if len(res) != 2 {
		return nil, fmt.Errorf("invalid cmdline with missing public_address: %s", cmdline)
	}

	return parseAddresses(strings.Split(res[1], ","))
}

// String gets the string representation of an address.
func (address ProcessAddress) String() string {
	if address.Port == 0 {
		return address.MachineAddress()
	}

	var sb strings.Builder
	// We have to do this since we are creating a template file for the processes.
	// The template file will contain variables like POD_IP which is not a valid net.IP :)
	sb.WriteString(net.JoinHostPort(address.MachineAddress(), strconv.Itoa(address.Port)))

	flags := address.SortedFlags()
	if len(flags) > 0 {
		sb.WriteString(":" + strings.Join(flags, ":"))
	}

	if address.FromHostname {
		sb.WriteString("(fromHostname)")
	}

	return sb.String()
}

// MachineAddress returns the machine address, this is the address without any ports.
func (address ProcessAddress) MachineAddress() string {
	if address.StringAddress == "" {
		return address.IPAddress.String()
	}

	return address.StringAddress
}

// StringWithoutFlags gets the string representation of an address without flags.
func (address ProcessAddress) StringWithoutFlags() string {
	if address.Port == 0 {
		return address.MachineAddress()
	}

	return net.JoinHostPort(address.MachineAddress(), strconv.Itoa(address.Port))
}

// GetProcessPort returns the expected port for a given process number
// and the tls setting.
func GetProcessPort(processNumber int, tls bool) int {
	if tls {
		return 4498 + 2*processNumber
	}

	return 4499 + 2*processNumber
}

// GetFullAddressList gets the full list of public addresses we should use for a
// process.
//
// This will include the IP address, the port, and any additional flags.
//
// If a process needs multiple addresses, this will include all of them,
// separated by commas. If you pass false for primaryOnly, this will return only
// the primary address.
func GetFullAddressList(address string, primaryOnly bool, processNumber int, requireTLS bool, requireNonTLS bool) []ProcessAddress {
	addrs := make([]ProcessAddress, 0, 2)

	// If the address is already enclosed in brackets, remove them since they
	// will be re-added automatically in the fdb.ProcessAddress logic.
	address = strings.TrimPrefix(strings.TrimSuffix(address, "]"), "[")

	// When a TLS address is provided the TLS address will always be the primary address
	// see: https://github.com/apple/foundationdb/blob/master/fdbrpc/FlowTransport.h#L49-L56
	if requireTLS {
		pAddr := NewProcessAddress(nil, address, GetProcessPort(processNumber, true), map[string]bool{"tls": true})
		addrs = append(addrs, pAddr)

		if primaryOnly {
			return addrs
		}
	}

	if requireNonTLS {
		pAddr := NewProcessAddress(nil, address, GetProcessPort(processNumber, false), nil)
		if !requireTLS && primaryOnly {
			return []ProcessAddress{pAddr}
		}

		addrs = append(addrs, pAddr)
	}

	return addrs
}
