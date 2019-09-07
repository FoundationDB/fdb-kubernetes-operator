#! /usr/bin/python

# entrypoint.py
#
# This source file is part of the FoundationDB open source project
#
# Copyright 2013-2018 Apple Inc. and the FoundationDB project authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

import argparse
import hashlib
import http.server
import json
import os
import shutil
import socket
import ssl
import stat
import traceback
from pathlib import Path

with open(os.path.join(os.getenv('SIDECAR_CONF_DIR'), 'config.json')) as conf_file:
    config = json.load(conf_file)
input_dir = os.getenv('INPUT_DIR', '/var/input-files')
output_dir = os.getenv('OUTPUT_DIR', '/var/output-files')

substitutions = {}
for key in ['FDB_PUBLIC_IP', 'FDB_MACHINE_ID', 'FDB_ZONE_ID']:
    substitutions[key] = os.getenv(key, '')

if substitutions['FDB_MACHINE_ID'] == '':
    substitutions['FDB_MACHINE_ID'] = os.getenv('HOSTNAME', '')

if substitutions['FDB_ZONE_ID'] == '':
    substitutions['FDB_ZONE_ID'] = substitutions['FDB_MACHINE_ID']
if substitutions['FDB_PUBLIC_IP'] == '':
    address_info = socket.getaddrinfo(substitutions['FDB_MACHINE_ID'], 4500, family=socket.AddressFamily.AF_INET)
    if len(address_info) > 0:
        substitutions['FDB_PUBLIC_IP'] = address_info[0][4][0]

if 'ADDITIONAL_SUBSTITUTIONS' in config and config['ADDITIONAL_SUBSTITUTIONS']:
    for key in config['ADDITIONAL_SUBSTITUTIONS']:
        substitutions[key] = os.getenv(key, key)

class Server(http.server.BaseHTTPRequestHandler):
    options_cache = None
    peer_verification_rules = None

    @classmethod
    def options(cls):
        '''
        This method gets the command-line options for the server.
        '''
        if cls.options_cache is not None:
            return cls.options_cache
        cls.options_cache = {}

        parser = argparse.ArgumentParser(description='FoundationDB Kubernetes Sidecar')
        parser.add_argument('--bind-address', help='IP and port to bind on',
                            default='0.0.0.0:8080')
        parser.add_argument('--tls',
                            help=('This flag enables TLS for incoming '
                                  'connections'),
                            action='store_true')
        parser.add_argument('--tls-certificate-file',
                            help=('The path to the certificate file for TLS '
                                  'connections. If this is not provided we '
                                  'will take the path from the '
                                  'FDB_TLS_CERTIFICATE_FILE environment '
                                  'variable.'))
        parser.add_argument('--tls-ca-file',
                            help=('The path to the certificate authority file '
                                  'for TLS connections  If this is not '
                                  'provided we will take the path from the '
                                  'FDB_TLS_CA_FILE environment variable.'))
        parser.add_argument('--tls-key-file',
                            help=('The path to the key file for TLS '
                                  'connections. If this is not provided we '
                                  'will take the path from the '
                                  'FDB_TLS_CERTIFICATE_FILE environment '
                                  'variable.'))
        parser.add_argument('--tls-verify-peers',
                            help=('The peer verification rules for incoming '
                                  'TLS  connections. If this is not provided we '
                                  'will take the rules from the '
                                  'FDB_TLS_VERIFY_PEERS environment variable. '
                                  'The format of this is the same as the TLS '
                                  'peer verification rules in FoundationDB.'))
        cls.options_cache = parser.parse_args()
        return cls.options_cache

    @classmethod
    def tls_args(cls):
        '''
        This method constructs the TLS file paths for a TLS session.
        '''
        options = cls.options()
        certificate_file = options.tls_certificate_file or os.getenv('FDB_TLS_CERTIFICATE_FILE')
        assert certificate_file, (
            "You must provide a certificate file, either through the "
            "tls_certificate_file argument or the FDB_TLS_CERTIFICATE_FILE "
            "environment variable")
        ca_file = options.tls_ca_file or os.getenv('FDB_TLS_CA_FILE')
        assert ca_file, (
            "You must provide a CA file, either through the tls_ca_file "
            "argument or the FDB_TLS_CA_FILE environment variable")
        key_file = options.tls_key_file or os.getenv('FDB_TLS_KEY_FILE')
        assert key_file, (
            "You must provide a key file, either through the tls_key_file "
            "argument or the FDB_TLS_KEY_FILE environment variable")
        return (certificate_file, ca_file, key_file)

    @classmethod
    def start(cls):
        '''
        This method starts the server.
        '''
        options = cls.options()
        (address, port) = options.bind_address.split(':')
        print('Listening on %s:%s' % (address, port))
        httpd = http.server.HTTPServer((address, int(port)), cls)

        if options.tls:
            (certificate_file, ca_file, key_file) = cls.tls_args()
            context = ssl.create_default_context(cafile=ca_file)
            context.check_hostname = False
            context.verify_mode = ssl.CERT_REQUIRED
            context.load_cert_chain(certificate_file, key_file)
            cls.peer_verification_rules = options.tls_verify_peers \
                or os.getenv('FDB_TLS_VERIFY_PEERS')
            httpd.socket = context.wrap_socket(httpd.socket, server_side=True)
        httpd.serve_forever()

    def send_text(self, text, code=200, content_type='text/plain',
                  add_newline=True):
        '''
        This method sends a text response.
        '''
        if add_newline:
            text += "\n"

        self.send_response(code)
        response = bytes(text, encoding='utf-8')
        self.send_header('Content-Length', str(len(response)))
        self.send_header('Content-Type', content_type)
        self.end_headers()
        self.wfile.write(response)

    def check_request_cert(self):
        approved = not Server.options().tls or self.check_cert(self.connection.getpeercert(), Server.peer_verification_rules)
        if not approved:
            self.send_error(401, 'Client certificate was not approved')
        return approved

    def check_cert(self, cert, rules):
        '''
        This method checks that the client's certificate is valid.

        If there is any problem with the certificate, this will return a string
        describing the error.
        '''
        if not rules:
            return True

        for option in rules.split(';'):
            option_valid = True
            for rule in option.split(','):
                if not self.check_cert_rule(cert, rule):
                    option_valid = False
                    break

            if option_valid:
                return True

        return False

    def check_cert_rule(self, cert, rule):
        (key, expected_value) = rule.split('=', 1)
        if '.' in key:
            (scope_key, field_key) = key.split('.', 1)
        else:
            scope_key = 'S'
            field_key = key

        if scope_key == 'S' or scope_key == 'Subject':
            scope_name = 'subject'
        elif scope_key == 'I' or scope_key == 'Issuer':
            scope_name = 'issuer'
        elif scope_key == 'R' or scope_key == 'Root':
            scope_name = 'root'
        else:
            assert False, 'Unknown certificate scope %s' % scope_key

        if not scope_name in cert:
            return False

        rdns = None
        operator = ''
        if field_key == 'CN':
            field_name = 'commonName'
        elif field_key == 'C':
            field_name = 'country'
        elif field_key == 'L':
            field_name = 'localityName'
        elif field_key == 'ST':
            field_name = 'stateOrProvinceName'
        elif field_key == 'O':
            field_name = 'organizationName'
        elif field_key == 'OU':
            field_name = 'organizationalUnitName'
        elif field_key == 'UID':
            field_name = 'userId'
        elif field_key == 'DC':
            field_name = 'domainComponent'
        elif field_key.startswith('subjectAltName') and scope_name == 'subject':
            operator = field_key[14:]
            field_key = field_key[0:14]
            (field_name, expected_value) = expected_value.split(':', 1)
            if not field_key in cert:
                return False
            rdns = [cert['subjectAltName']]
        else:
            assert False, 'Unknown certificate field %s' % field_key

        if not rdns:
            rdns = list(cert[scope_name])

        for rdn in rdns:
            for entry in list(rdn):
                if entry[0] == field_name:
                    if operator == '' and entry[1] == expected_value:
                        return True
                    elif operator == '<' and entry[1].endswith(expected_value):
                        return True
                    elif operator == '>' and entry[1].startswith(expected_value):
                        return True


    def do_GET(self):
        '''
        This method executes a GET request.
        '''
        try:
            if not self.check_request_cert():
                return
            if self.path.startswith('/check_hash/'):
                self.send_text(check_hash(self.path[12:]), add_newline=False)
            elif self.path == "/ready":
                self.send_text(ready())
            elif self.path == '/substitutions':
                self.send_text(get_substitutions())
            else:
                self.send_error(404, "Path not found")
                self.end_headers()
        except:
            traceback.print_exc()
            self.send_error(500)
            self.end_headers()


    def do_POST(self):
        '''
        This method executes a POST request.
        '''
        try:
            if not self.check_request_cert():
                return
            if self.path == '/copy_files':
                self.send_text(copy_files())
            elif self.path == '/copy_binaries':
                self.send_text(copy_binaries())
            elif self.path == '/copy_libraries':
                self.send_text(copy_libraries())
            elif self.path == '/copy_monitor_conf':
                self.send_text(copy_monitor_conf())
            else:
                self.send_error(404, "Path not found")
                self.end_headers()
        except:
            traceback.print_exc()
            self.send_error(500)
            self.end_headers()

def check_hash(filename):
    try:
        with open('%s/%s' % (output_dir, filename), 'rb') as contents:
            m = hashlib.sha256()
            m.update(contents.read())
            return m.hexdigest()
    except FileNotFoundError:
        raise

def copy_files():
    for filename in config['COPY_FILES']:
        shutil.copy('%s/%s' % (input_dir, filename), '%s/%s' % (output_dir, filename))
    return "OK"

def copy_binaries():
    with open('/var/fdb/version') as version_file:
        primary_version = version_file.read().strip()
    for binary in config['COPY_BINARIES']:
        path = Path('/usr/bin/%s' % binary)
        target_path = Path('%s/bin/%s/%s' % (output_dir, primary_version, binary))
        if not target_path.exists():
            target_path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(path, target_path)
            target_path.chmod(0o744)
    return "OK"

def copy_libraries():
    for version in config['COPY_LIBRARIES']:
        path =  Path('/var/fdb/lib/libfdb_c_%s.so' % version)
        if version == config['COPY_LIBRARIES'][0]:
            target_path = Path('%s/lib/libfdb_c.so' % (output_dir))
        else:
            target_path = Path('%s/lib/multiversion/libfdb_c_%s.so' % (output_dir, version))
        if not target_path.exists():
            target_path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(path, target_path)
    return "OK"

def copy_monitor_conf():
    if 'INPUT_MONITOR_CONF' in config and config['INPUT_MONITOR_CONF']:
        with open('%s/%s' % (input_dir, config['INPUT_MONITOR_CONF'])) as monitor_conf_file:
            monitor_conf = monitor_conf_file.read()
        for variable in substitutions:
            monitor_conf = monitor_conf.replace('$' + variable, substitutions[variable])
        with open('%s/fdbmonitor.conf' % output_dir, 'w') as output_conf_file:
            output_conf_file.write(monitor_conf)
    return "OK"

def get_substitutions():
    return json.dumps(substitutions)

def ready():
    return "OK"

if __name__ == '__main__':
    copy_files()
    copy_binaries()
    copy_libraries()
    copy_monitor_conf()

    if os.getenv('COPY_ONCE') != '1':
        Server.start()
