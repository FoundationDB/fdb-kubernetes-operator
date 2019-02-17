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

import hashlib
import os
import shutil
import stat
from pathlib import Path

import flask

app = flask.Flask(__name__)
app.config.from_json(os.getenv('SIDECAR_CONF_DIR') + '/config.json')

INPUT_DIR = os.getenv('INPUT_DIR', '/var/input-files')
OUTPUT_DIR = os.getenv('OUTPUT_DIR', '/var/output-files')
MONITOR_CONF_VARIABLES = ['FDB_PUBLIC_IP', 'HOSTNAME']

@app.route('/check_hash/<filename>')
def check_hash(filename):
	try:
		with open('%s/%s' % (OUTPUT_DIR, filename)) as contents:
			m = hashlib.sha256()
			m.update(contents.read().encode('utf-8'))
			return m.hexdigest()
	except FileNotFoundError:
		flask.abort(404)

@app.route('/copy_files', methods=['POST'])
def copy_files():
	for filename in app.config['COPY_FILES']:
		shutil.copy('%s/%s' % (INPUT_DIR, filename), '%s/%s' % (OUTPUT_DIR, filename))
	return "OK"

@app.route('/copy_binaries', methods=['POST'])
def copy_binaries():
	with open('/var/fdb/version') as version_file:
		primary_version = version_file.read().strip()
	for binary in app.config['COPY_BINARIES']:
		path = Path('/usr/bin/%s' % binary)
		target_path = Path('%s/bin/%s/%s' % (OUTPUT_DIR, primary_version, binary))
		if not target_path.exists():
			target_path.parent.mkdir(parents=True, exist_ok=True)
			shutil.copy(path, target_path)
			target_path.chmod(0o744)
	return "OK"

@app.route('/copy_libraries', methods=['POST'])
def copy_libraries():
	for version in app.config['COPY_LIBRARIES']:
		path =  Path('/var/fdb/lib/libfdb_c_%s.so' % version)
		if version == app.config['COPY_LIBRARIES'][0]:
			target_path = Path('%s/lib/libfdb_c.so' % (OUTPUT_DIR))
		else:
			target_path = Path('%s/lib/multiversion/libfdb_c_%s.so' % (OUTPUT_DIR, version))
		if not target_path.exists():
			target_path.parent.mkdir(parents=True, exist_ok=True)
			shutil.copy(path, target_path)
	return "OK"

@app.route("/copy_monitor_conf", methods=['POST'])
def copy_monitor_conf():
    if app.config['INPUT_MONITOR_CONF']:
        with open('%s/%s' % (INPUT_DIR, app.config['INPUT_MONITOR_CONF'])) as monitor_conf_file:
            monitor_conf = monitor_conf_file.read()
        for variable in MONITOR_CONF_VARIABLES:
            monitor_conf = monitor_conf.replace('$' + variable, os.getenv(variable))
        with open('%s/fdbmonitor.conf' % OUTPUT_DIR, 'w') as output_conf_file:
            output_conf_file.write(monitor_conf)
    return "OK"

@app.route('/ready')
def ready():
	return "OK"

copy_files()
copy_binaries()
copy_libraries()
copy_monitor_conf()