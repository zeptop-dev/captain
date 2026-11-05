#!/usr/bin/env python3
"""Exercise the real installer with isolated files and fake Docker/host commands."""
import fcntl
import itertools
import json
import os
from pathlib import Path
import shutil
import ssl
import http.server
import threading
import subprocess
import tempfile
import types
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
INSTALLER = (ROOT / 'install.sh').read_text()
HELPER = INSTALLER.split('# BEGIN AGENT HTTPS HELPER\n', 1)[1].split('# END AGENT HTTPS HELPER', 1)[0]


class ExistingInstall(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='captain-agent-https-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.directory = self.root / 'captain'
        self.directory.mkdir()
        self.base = self.directory / 'docker-compose.yml'
        self.base.write_text('services:\n  captain:\n    image: zeptop/captain:latest\n')
        self.custom = self.directory / 'compose.custom.yaml'
        self.custom.write_text('services:\n  captain:\n    networks: [proxy]\n')
        self.original_base = self.base.read_bytes()
        self.original_custom = self.custom.read_bytes()
        self.env = self.directory / '.env'
        self.files = [str(self.base), str(self.custom)]
        self.env.write_text('KEEP_THIS=fixture-value\nCOMPOSE_FILE=' + ':'.join(self.files) + '\n')
        self.item = {'Image': 'sha256:running', 'State': {'Running': True}, 'Config': {'Labels': {
            'com.docker.compose.project': 'captain',
            'com.docker.compose.project.working_dir': str(self.directory),
            'com.docker.compose.project.config_files': ','.join(self.files),
            'com.docker.compose.config-hash': 'hash',
        }}, 'HostConfig': {'NetworkMode': 'bridge', 'PortBindings': {'8080/tcp': [{'HostPort': '8080', 'HostIp': '127.0.0.1'}]}}}
        self.module = types.ModuleType('agent_https_helper')
        exec(compile(HELPER, str(ROOT / 'install.sh'), 'exec'), self.module.__dict__)
        self.module.UPDATER_DIR = self.root / 'updater-config'
        self.module.UPDATER_STATE = self.root / 'updater-state'
        self.module.UPDATER_WORKER = self.root / 'worker'
        self.module.trusted = mock.Mock()
        self.module.port_available = mock.Mock()
        self.real_firewall = self.module.ensure_firewall
        self.module.ensure_firewall = mock.Mock(return_value=True)
        self.calls = []
        self.config_hash = 'hash'
        self.local_image = 'sha256:running'
        self.fail_up = False
        self.extra_ports = []
        self.up_count = 0
        self.after_up = None
        self.module.run = self.run_command
        self.environment = mock.patch.dict(os.environ, {k: '' for k in ('COMPOSE_FILE', 'COMPOSE_PROJECT_NAME', 'COMPOSE_ENV_FILES', 'COMPOSE_PATH_SEPARATOR')})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def run_command(self, args):
        self.calls.append(args)
        if args[:2] == ['docker', 'ps']:
            return 'captain-id\n'
        if args[:2] == ['docker', 'inspect']:
            return json.dumps([self.item])
        if args[:3] == ['docker', 'image', 'inspect']:
            return json.dumps([{'Id': self.local_image}])
        if args[0] == str(self.module.UPDATER_WORKER):
            return ''
        if args[:2] == ['docker', 'compose']:
            if args[-3:] == ['config', '--format', 'json']:
                return json.dumps({'services': {'captain': {'image': 'zeptop/captain:latest', 'ports': [{'published': '8080', 'target': 8080, 'host_ip': '127.0.0.1'}] + self.extra_ports}}})
            if args[-3:] == ['config', '--hash', 'captain']:
                return 'captain ' + self.config_hash + '\n'
            if args[-2:] == ['config', '--quiet']:
                return ''
            if args[-3:] == ['ps', '--quiet', 'captain']:
                return 'captain-id\n'
            if 'up' in args:
                self.up_count += 1
                if self.fail_up:
                    self.fail_up = False
                    raise ValueError('Docker failed')
                paths = [args[i + 1] for i, arg in enumerate(args) if arg == '--file']
                self.item['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(paths)
                self.item['HostConfig']['PortBindings'] = {'8080/tcp': [{'HostPort': '8080', 'HostIp': '127.0.0.1'}]}
                override = self.directory / self.module.OVERRIDE
                if str(override) in paths:
                    port = override.read_text().split('"')[1].split(':')[0]
                    self.item['HostConfig']['PortBindings'][port + '/tcp'] = [{'HostPort': port, 'HostIp': '0.0.0.0'}]
                if self.after_up:
                    self.after_up(self.item, self.up_count)
                return ''
        raise AssertionError('unexpected command: ' + repr(args))

    def apply(self, port='2096'):
        with mock.patch('builtins.print'):
            self.module.main(port, str(self.directory))

    def add_updater(self):
        self.module.UPDATER_DIR.mkdir()
        self.module.UPDATER_STATE.mkdir()
        self.module.UPDATER_WORKER.write_text('fixture worker')
        (self.module.UPDATER_DIR / 'config.json').write_text(json.dumps({'dir': str(self.directory), 'project': 'captain'}))
        (self.module.UPDATER_STATE / 'job.json').write_text(json.dumps({'id': 'previous-job', 'phase': 'complete'}))
        pin = self.directory / 'compose.updater.yaml'
        pin.write_text('services:\n  captain:\n    image: sha256:running\n')
        self.files.append(str(pin))
        self.item['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(self.files)
        self.env.write_text('KEEP_THIS=fixture-value\nCOMPOSE_FILE=' + ':'.join(self.files) + '\n')
        return pin

    def test_preserve_existing_deployment_and_repeat(self):
        self.apply()
        new_env = self.env.read_bytes()
        self.assertIn(b'KEEP_THIS=fixture-value', new_env)
        self.assertEqual(self.original_base, self.base.read_bytes())
        self.assertEqual(self.original_custom, self.custom.read_bytes())
        self.assertEqual('sha256:running', self.item['Image'])
        self.assertIn('8080/tcp', self.item['HostConfig']['PortBindings'])
        self.apply()
        self.assertEqual(new_env, self.env.read_bytes())
        self.assertEqual(1, self.env.read_text().count('compose.agent-https.yaml'))
        self.assertFalse(any('pull' in args or 'admin' in args or 'upgrade' in args for args in self.calls))
        self.assertTrue(all('--no-deps' in args and '--no-build' in args and '--pull' in args for args in self.calls if 'up' in args))

    def test_port_change_replaces_only_managed_mapping(self):
        self.apply()
        self.apply('2083')
        self.assertNotIn('2096/tcp', self.item['HostConfig']['PortBindings'])
        self.assertIn('2083/tcp', self.item['HostConfig']['PortBindings'])
        self.assertIn('8080/tcp', self.item['HostConfig']['PortBindings'])

    def test_stopped_updater_preserves_pin_last_and_registers(self):
        pin = self.add_updater()
        pin_data = pin.read_bytes()
        self.apply()
        self.assertTrue(self.env.read_text().rstrip().endswith(str(pin)))
        self.assertEqual(pin_data, pin.read_bytes())
        self.assertIn([str(self.module.UPDATER_WORKER), 'docker-updater', 'setup', '--dir', str(self.directory)], self.calls)

    def test_running_updater_refused_without_changes(self):
        self.add_updater()
        before = self.env.read_bytes()
        with (self.module.UPDATER_STATE / 'worker.lock').open('w') as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, 'updater is running'):
                self.apply()
        self.assertEqual(before, self.env.read_bytes())
        self.assertFalse((self.directory / self.module.OVERRIDE).exists())

    def test_incomplete_or_failed_updater_job_refused(self):
        self.add_updater()
        for phase in ('checking', 'backing_up', 'recreating', 'failed'):
            (self.module.UPDATER_STATE / 'job.json').write_text(json.dumps({'id': 'job', 'phase': phase}))
            with self.assertRaisesRegex(ValueError, 'unfinished updater job'):
                self.apply()

    def test_config_or_image_drift_refused(self):
        self.config_hash = 'edited'
        with self.assertRaisesRegex(ValueError, 'configuration changed'):
            self.apply()
        self.config_hash = 'hash'
        self.local_image = 'sha256:newer'
        with self.assertRaisesRegex(ValueError, 'local image differs'):
            self.apply()
        self.assertFalse((self.directory / self.module.OVERRIDE).exists())

    def test_failed_recreate_restores_files_and_service(self):
        before = self.env.read_bytes()
        self.fail_up = True
        with self.assertRaisesRegex(ValueError, 'original deployment files and service restored'):
            self.apply()
        self.assertEqual(before, self.env.read_bytes())
        self.assertFalse((self.directory / self.module.OVERRIDE).exists())
        self.assertEqual(self.files, self.item['Config']['Labels']['com.docker.compose.project.config_files'].split(','))

    def test_conflict_unknown_override_and_invalid_port(self):
        self.extra_ports = [{'published': '2096', 'target': 443}]
        with self.assertRaisesRegex(ValueError, 'conflicts'):
            self.apply()
        self.extra_ports = []
        self.module.port_available.side_effect = ValueError('occupied')
        with self.assertRaisesRegex(ValueError, 'occupied'):
            self.apply()
        self.module.port_available.side_effect = None
        (self.directory / self.module.OVERRIDE).write_text('custom configuration')
        with self.assertRaisesRegex(ValueError, 'not installer-managed'):
            self.apply()
        for port in ('0', '443', '8080', '65536', '02096', '2096;false', ''):
            with self.subTest(port=port), self.assertRaisesRegex(ValueError, 'port must be'):
                self.apply(port)

    def test_multiline_env_preserved_byte_for_byte(self):
        prefix = b"# keep CRLF and Unicode\r\nKEY='first line\nCOMPOSE_FILE=inside-secret\nCOMPOSE_ENV_FILES=also-inside\nlast line'\r\nOTHER=\xe5\x80\xbc\r\n"
        suffix = b"AFTER=still-here\n\n"
        original = prefix + b'COMPOSE_FILE="' + ':'.join(self.files).encode() + b'" # old declaration\r\n' + suffix
        self.env.write_bytes(original)
        self.apply()
        data = self.env.read_bytes()
        self.assertTrue(data.startswith(prefix))
        self.assertTrue(data.endswith(suffix))
        self.assertIn(b'COMPOSE_FILE=inside-secret', data)
        self.assertIn(b'compose.agent-https.yaml\r\n', data)
        self.apply()
        self.assertEqual(data, self.env.read_bytes())

    def test_simple_quoted_compose_files_and_comments(self):
        for quote in ("'", '"'):
            self.env.write_text('KEEP=unchanged\nexport COMPOSE_FILE=' + quote + ':'.join(self.files) + quote + ' # a comment\n')
            self.item['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(self.files)
            self.apply()
            self.assertTrue(self.env.read_text().startswith('KEEP=unchanged\nCOMPOSE_FILE='))

    def test_ambiguous_env_refused_without_mutation(self):
        for content in ("KEY='unterminated\nCOMPOSE_FILE=wrong\n", 'KEY=continued\\\nCOMPOSE_FILE=wrong\n', 'not an assignment\n'):
            with self.subTest(content=content):
                self.env.write_text(content)
                with self.assertRaises(ValueError):
                    self.apply()
                self.assertEqual(content, self.env.read_text())
                self.assertFalse((self.directory / self.module.OVERRIDE).exists())

    def test_legacy_updater_cannot_rewrite_embedded_compose_line(self):
        self.add_updater()
        self.env.write_text("KEY='multiline\nCOMPOSE_FILE=inside-value\nend'\n" + 'COMPOSE_FILE=' + ':'.join(self.files) + '\n')
        before = self.env.read_bytes()
        with self.assertRaisesRegex(ValueError, 'cannot preserve this multiline'):
            self.apply()
        self.assertEqual(before, self.env.read_bytes())
        self.assertFalse((self.directory / self.module.OVERRIDE).exists())

    def test_rollback_checks_actual_running_image_ports_and_identity(self):
        changes = [
            lambda item: item['State'].update(Running=False),
            lambda item: item.update(Image='sha256:unexpected'),
            lambda item: item['HostConfig'].update(PortBindings={}),
            lambda item: item['Config']['Labels'].update({'com.docker.compose.project': 'unexpected'}),
        ]
        original = json.loads(json.dumps(self.item))
        for change in changes:
            with self.subTest(change=change):
                self.item = json.loads(json.dumps(original))
                self.fail_up = True
                self.after_up = lambda item, count: change(item)
                with self.assertRaisesRegex(ValueError, 'container recovery failed'):
                    self.apply()

    def health_clock(self):
        self.item['Config']['Healthcheck'] = {'Test': ['CMD', 'captain', 'healthcheck']}
        self.item['State']['Health'] = {'Status': 'healthy'}
        self.module.time = types.SimpleNamespace(monotonic=mock.Mock(side_effect=itertools.count(0, 60)), sleep=mock.Mock())

    def test_success_waits_for_existing_healthcheck(self):
        self.health_clock()
        self.after_up = lambda item, count: item['State'].update(Health={'Status': 'starting'})
        self.module.time.sleep.side_effect = lambda seconds: self.item['State'].update(Health={'Status': 'healthy'})
        self.apply()
        self.module.time.sleep.assert_called_once_with(1)

    def test_unhealthy_success_rolls_back_and_waits_for_recovery_health(self):
        self.health_clock()
        def status(item, count):
            item['State']['Health'] = {'Status': 'starting' if count == 1 else 'healthy'}
        self.after_up = status
        with self.assertRaisesRegex(ValueError, 'original deployment files and service restored'):
            self.apply()
        self.assertEqual(2, self.up_count)

    def test_unhealthy_rollback_never_reports_restored(self):
        self.health_clock()
        self.fail_up = True
        self.after_up = lambda item, count: item['State'].update(Health={'Status': 'unhealthy'})
        with self.assertRaisesRegex(ValueError, 'container recovery failed'):
            self.apply()

    def test_deployment_lock_covers_checks_and_updater_registration(self):
        self.add_updater()
        original_run = self.module.run
        checked = []
        def concurrent(args):
            if args[:2] == ['docker', 'ps'] or args[0] == str(self.module.UPDATER_WORKER):
                before = self.env.read_bytes()
                with self.assertRaisesRegex(ValueError, 'another Agent HTTPS deployment operation'):
                    self.module.main('2083', str(self.directory))
                self.assertEqual(before, self.env.read_bytes())
                checked.append(args[0])
            return original_run(args)
        self.module.run = concurrent
        self.apply()
        self.assertEqual(['docker', str(self.module.UPDATER_WORKER)], checked)
        self.assertIn('2096/tcp', self.item['HostConfig']['PortBindings'])
        self.assertNotIn('2083/tcp', self.item['HostConfig']['PortBindings'])

    def test_host_mode_needs_no_mutation(self):
        self.item['HostConfig']['NetworkMode'] = 'host'
        before = self.env.read_bytes()
        self.apply()
        self.assertEqual(before, self.env.read_bytes())
        self.assertFalse((self.directory / self.module.OVERRIDE).exists())
        self.module.port_available.assert_not_called()


class HostAccess(unittest.TestCase):
    run_command = ExistingInstall.run_command
    add_updater = ExistingInstall.add_updater

    def setUp(self):
        ExistingInstall.setUp(self)
        self.module.ensure_firewall = self.real_firewall
        self.module.FW_LOCK = self.root / 'firewall.lock'
        self.module.UFW_DEFAULTS = self.root / 'ufw-defaults'
        self.module.UFW_DEFAULTS.write_text('IPV6=yes\n')
        self.available = ['ufw']
        self.module.shutil = types.SimpleNamespace(which=lambda name: name if name in self.available else None)
        self.ufw_rules = []
        self.rich_rules = {False: set(), True: set()}
        self.plain_ports = {False: False, True: False}
        self.zone_output = 'public\n  interfaces: eth0\n'
        self.firewall_commands = []
        self.fail_permanent = False
        self.fail_cleanup = False
        self.module.firewall_command = self.firewall_command
        self.health_results = [True, True]
        self.module.probe_health = mock.Mock(side_effect=lambda origin, local=False: self.health_results[0 if local else 1])

    def firewall_command(self, args):
        self.firewall_commands.append(args)
        code, output = 0, ''
        if args == ['ufw', 'status']:
            output = 'Status: active\n' + '\n'.join(self.ufw_rules) + '\n'
        elif args[0] == 'ufw':
            deleting = '--force' in args
            marker = args[-1]
            if deleting:
                self.ufw_rules = [line for line in self.ufw_rules if not line.endswith('# ' + marker)]
            else:
                family = ' (v6)' if '::/0' in args else ''
                self.ufw_rules.append('2096/tcp' + family + ' ALLOW IN Anywhere' + family + ' # ' + marker)
        elif args == ['firewall-cmd', '--state']:
            output = 'running\n'
        elif args == ['firewall-cmd', '--get-active-zones']:
            output = self.zone_output
        elif args[0] == 'firewall-cmd':
            permanent = '--permanent' in args
            operation = args[-1]
            if operation.startswith('--query-port='):
                code = 0 if self.plain_ports[permanent] else 1
            elif operation.startswith('--query-rich-rule='):
                code = 0 if operation.split('=', 1)[1] in self.rich_rules[permanent] else 1
            elif operation.startswith('--add-rich-rule='):
                if permanent and self.fail_permanent:
                    code = 1
                else:
                    self.rich_rules[permanent].add(operation.split('=', 1)[1])
            elif operation.startswith('--remove-rich-rule='):
                if self.fail_cleanup:
                    code = 1
                else:
                    self.rich_rules[permanent].discard(operation.split('=', 1)[1])
            else:
                raise AssertionError(args)
        else:
            raise AssertionError(args)
        return types.SimpleNamespace(returncode=code, stdout=output, stderr='')

    def host_call(self, action='enable-agent-https', firewall='auto', url='https://panel.example.com:2096'):
        with mock.patch('builtins.print') as output:
            result = self.module.main('2096', str(self.directory), action, firewall, url)
        return result, str(output.call_args_list)

    def test_ufw_per_family_idempotence_and_user_rule_preservation(self):
        user_rule = '2096/tcp (v6) ALLOW IN Anywhere (v6) # user-owned'
        self.ufw_rules = [user_rule]
        result, _ = self.host_call()
        self.assertTrue(result)
        self.assertIn(user_rule, self.ufw_rules)
        self.assertEqual(1, len([args for args in self.firewall_commands if args[0:2] == ['ufw', 'allow']]))
        record = json.loads((self.directory / self.module.FW_STATE).read_text())
        self.assertEqual(1, len(record['rules']))
        self.assertEqual('ufw', record['rules'][0]['manager'])
        self.firewall_commands.clear()
        self.host_call()
        self.assertFalse(any(args[:2] == ['ufw', 'allow'] for args in self.firewall_commands))

    def test_firewalld_runtime_permanent_preserves_plain_user_rules(self):
        self.available = ['firewall-cmd']
        self.plain_ports[False] = True
        self.assertTrue(self.host_call()[0])
        self.assertEqual(set(), self.rich_rules[False])
        self.assertEqual(1, len(self.rich_rules[True]))
        self.firewall_commands.clear()
        self.assertTrue(self.host_call()[0])
        self.assertFalse(any('--add-' in arg for args in self.firewall_commands for arg in args))
        self.assertTrue(self.plain_ports[False])

    def test_firewalld_failed_second_write_removes_only_new_rich_rule(self):
        self.available = ['firewall-cmd']
        self.rich_rules[False].add('user-owned-existing-rule')
        self.fail_permanent = True
        result, output = self.host_call()
        self.assertFalse(result)
        self.assertEqual({'user-owned-existing-rule'}, self.rich_rules[False])
        self.assertIn('rule update failed', output)
        self.assertIn('2096/tcp', self.item['HostConfig']['PortBindings'])
        self.assertFalse(any('--remove-port' in arg for args in self.firewall_commands for arg in args))

    def test_failed_cleanup_keeps_owned_record(self):
        self.available = ['firewall-cmd']
        self.fail_permanent = self.fail_cleanup = True
        result, output = self.host_call()
        self.assertFalse(result)
        self.assertIn('cleanup was incomplete', output)
        record = json.loads((self.directory / self.module.FW_STATE).read_text())
        self.assertEqual(2, len(record['rules']))
        self.assertIn(record['rules'][0]['rich_rule'], self.rich_rules[False])

    def test_unknown_multiple_managers_or_zones_no_writes(self):
        for available, zones in (([], ''), (['ufw', 'firewall-cmd'], ''), (['firewall-cmd'], 'public\n  interfaces: eth0\nprivate\n  interfaces: eth1\n')):
            self.available, self.zone_output = available, zones
            self.firewall_commands.clear()
            result, output = self.host_call()
            self.assertFalse(result)
            self.assertIn('unknown', output)
            self.assertFalse(any('allow' in args or any('--add-' in arg for arg in args) for args in self.firewall_commands))

    def test_host_mode_configures_firewall_without_recreate(self):
        self.item['HostConfig']['NetworkMode'] = 'host'
        before = self.env.read_bytes()
        self.assertTrue(self.host_call()[0])
        self.assertEqual(before, self.env.read_bytes())
        self.assertEqual(0, self.up_count)
        self.assertEqual(2, len(self.ufw_rules))

    def test_read_only_checks_write_nothing(self):
        before = {p.name: p.read_bytes() for p in self.directory.iterdir()}
        result, output = self.host_call(action='check-agent-https')
        self.assertFalse(result)
        self.assertIn('docker_mapping', output)
        self.assertEqual(before, {p.name: p.read_bytes() for p in self.directory.iterdir()})
        self.assertFalse(self.module.FW_LOCK.exists())
        self.assertFalse(any('allow' in args for args in self.firewall_commands))
        self.assertEqual(0, self.up_count)

    def test_failed_network_checks_keep_completed_mapping(self):
        self.health_results = [False, False]
        result, output = self.host_call(firewall='skip')
        self.assertFalse(result)
        self.assertIn('host_tls', output)
        self.assertIn('domain_health', output)
        self.assertIn('retained', output)
        self.assertIn('2096/tcp', self.item['HostConfig']['PortBindings'])
        self.assertEqual(1, self.up_count)

    def test_unsafe_origins_rejected_before_any_mutation(self):
        before = set(self.directory.iterdir())
        for value in ('http://panel.example.com:2096', 'https://u:p@panel.example.com:2096', 'https://panel.example.com:2096/path', 'https://panel.example.com:2096?x', 'https://panel.example.com:2096#x', 'https://panel.example.com:2083', 'https://panel.example.com:2096\n'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.host_call(url=value)
        self.assertEqual(before, set(self.directory.iterdir()))
        self.assertEqual([], self.calls)

    def test_health_response_requires_exact_contract_and_no_redirect(self):
        # Use the actual probe function, with only the network connection faked.
        module = types.ModuleType('actual_probe')
        exec(compile(HELPER, 'helper', 'exec'), module.__dict__)
        origin = module.validate_origin('https://panel.example.com:2096', '2096')
        for status, data, accepted in ((200, b'{"ok":true,"service":"captain-agent"}', True), (301, b'{}', False), (200, b'{"ok":true}', False), (200, b'{"ok":1,"service":"captain-agent"}', False), (200, b'x' * 8193, False)):
            response = types.SimpleNamespace(status=status, read=lambda size: data[:size])
            connection = mock.Mock()
            connection.getresponse.return_value = response
            module.ProbeHTTPS = mock.Mock(return_value=connection)
            self.assertEqual(accepted, module.probe_health(origin, local=True))
            module.ProbeHTTPS.assert_called_once_with('panel.example.com', 2096, '127.0.0.1')
            connection.request.assert_called_once_with('GET', '/api/agent/health', headers={'Cache-Control': 'no-cache', 'Accept': 'application/json'})
            connection.close.assert_called_once()


class TLSHealth(unittest.TestCase):
    def test_real_tls_hostname_trust_and_sni(self):
        # Trust only the temporary fixture CA in the accepted request. Production
        # always uses the system trust store and has no insecure override flag.
        module = types.ModuleType('tls_health')
        exec(compile(HELPER, 'helper', 'exec'), module.__dict__)
        with tempfile.TemporaryDirectory(prefix='captain-health-tls-') as directory:
            root = Path(directory)
            config = root / 'openssl.cnf'
            config.write_text('[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=localhost\n[ext]\nsubjectAltName=DNS:localhost\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n')
            subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-keyout', str(root / 'key.pem'), '-out', str(root / 'cert.pem'), '-config', str(config)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            seen = []
            class Handler(http.server.BaseHTTPRequestHandler):
                def do_GET(self):
                    seen.append((self.path, self.headers['Host']))
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b'{"ok":true,"service":"captain-agent"}')
                def log_message(self, *args):
                    pass
            server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
            tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            tls.load_cert_chain(root / 'cert.pem', root / 'key.pem')
            sni = []
            tls.set_servername_callback(lambda sock, name, ctx: sni.append(name))
            server.socket = tls.wrap_socket(server.socket, server_side=True)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                port = server.server_address[1]
                origin = module.validate_origin('https://localhost:' + str(port), str(port))
                self.assertFalse(module.probe_health(origin, local=True))
                trusted = ssl.create_default_context(cafile=str(root / 'cert.pem'))
                with mock.patch.object(module.ssl, 'create_default_context', return_value=trusted):
                    self.assertTrue(module.probe_health(origin, local=True))
                    wrong = module.validate_origin('https://other.example.com:' + str(port), str(port))
                    self.assertFalse(module.probe_health(wrong, local=True))
                self.assertIn('localhost', sni)
                self.assertIn('other.example.com', sni)
                self.assertEqual([('/api/agent/health', 'localhost:' + str(port))], seen)
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)


class NewInstall(unittest.TestCase):
    def run_install(self, proxy=True, host=False, occupied=False, port='2096', container_proxy=False):
        with tempfile.TemporaryDirectory(prefix='captain-new-install-') as directory:
            root = Path(directory)
            bin_dir = root / 'bin'
            bin_dir.mkdir()
            fake = bin_dir / 'host-fake'
            fake.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
if name=='uname': print('Linux' if args==['-s'] else 'x86_64')
elif name=='id': print('0')
elif name=='ss':
    if os.environ.get('OCCUPIED')=='1' and 'sport = :2096' in args: print('LISTEN fixture')
    elif os.environ.get('CONTAINER_PROXY')=='1' and 'sport = :80' in args: print('LISTEN users:(("docker-proxy",pid=123,fd=1))')
elif name=='ps': print('docker-proxy')
elif name=='docker':
    if args[:1]==['ps'] and 'label=com.docker.compose.service=captain' in args: print('fixture-captain')
    elif args[:1]==['ps'] and 'publish=80' in args: print('openresty')
    elif args[:1]==['inspect'] and args[1]!='-f':
        print(json.dumps([{'State':{'Running':True},'Config':{'Labels':{'com.docker.compose.project.working_dir':os.environ['FIXTURE_DIR']+'/app'}},'HostConfig':{'NetworkMode':'host' if os.environ.get('HOST_NETWORK')=='1' else 'bridge','PortBindings':{'2096/tcp':[{'HostPort':'2096','HostIp':'0.0.0.0'}]}}}]))
    elif args[:1]==['inspect']:
        print('proxy-net' if 'range' in args[2] else 'bridge')
    elif args[:2]==['network','inspect']: print('10.10.0.2')
    elif args[:1]==['version']: print('27.0.0')
elif name=='ufw': print('Status: inactive')
elif name=='firewall-cmd': print('not running'); sys.exit(252)
elif name=='sed':
    if args[:1]==['-i']:
        path=pathlib.Path(args[-1]); path.write_text(''.join(line for line in path.read_text().splitlines(True) if line.strip()))
    else: os.execv(os.environ['REAL_SED'], ['sed']+args)
''')
            fake.chmod(0o755)
            for name in ('uname', 'id', 'ss', 'ps', 'docker', 'ip', 'chown', 'sleep', 'sed', 'ufw', 'firewall-cmd'):
                (bin_dir / name).symlink_to(fake)
            script = root / 'install.sh'
            # All installation paths are relocated before execution; no root or Docker operations occur.
            isolated = INSTALLER.replace('/opt/captain', str(root / 'app')).replace('/etc/captain', str(root / 'etc')).replace("Path('/run/captain-agent-access.lock')", "Path(" + repr(str(root / 'firewall.lock')) + ")")
            # Only this sandbox fixture relaxes root ownership; all commands,
            # deployment paths and firewall binaries remain isolated.
            begin, end = isolated.index('def trusted('), isolated.index('def run(')
            isolated = isolated[:begin] + 'def trusted(path, optional=False):\n    pass\n\n\n' + isolated[end:]
            script.write_text(isolated)
            args = ['sh', str(script), '--mode', 'docker', '--domain', 'panel.example.com', '--email', 'admin@example.com', '--admin-email', 'admin@example.com', '--admin-password', 'fixture-password', '--agent-https-port', port]
            if proxy:
                args.append('--behind-proxy')
            if host:
                args.extend(['--network', 'host'])
            env = dict(os.environ, PATH=str(bin_dir) + ':' + os.environ['PATH'], REAL_SED=shutil.which('sed'), OCCUPIED='1' if occupied else '0', CONTAINER_PROXY='1' if container_proxy else '0', FIXTURE_DIR=str(root), HOST_NETWORK='1' if host else '0')
            out = subprocess.run(args, capture_output=True, text=True, env=env, timeout=20)
            compose = root / 'app/docker-compose.yml'
            config = root / 'app/config.yaml'
            return out, compose.read_text() if compose.exists() else '', config.read_text() if config.exists() else ''

    def test_bridge_and_proxy_networks(self):
        for proxy, container_proxy in ((False, False), (True, False), (True, True)):
            with self.subTest(proxy=proxy, container_proxy=container_proxy):
                result, compose, config = self.run_install(proxy=proxy, container_proxy=container_proxy)
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertIn('"2096:2096/tcp"', compose)
                self.assertIn('"127.0.0.1:8080:8080"' if proxy else '"443:443"', compose)
                self.assertNotIn('agent_https', config)
                self.assertIn('[docker_mapping] ready', result.stdout)
                self.assertIn('[host_firewall] unknown', result.stdout)
                self.assertIn('[host_tls] not_checked', result.stdout)
                if container_proxy:
                    self.assertIn('networks: [default, proxy-net]', compose)

    def test_host_and_host_with_proxy(self):
        for container_proxy in (False, True):
            result, compose, _ = self.run_install(host=True, container_proxy=container_proxy)
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn('network_mode: host', compose)
            self.assertNotIn('ports:', compose)
            self.assertIn('[docker_mapping] host', result.stdout)
            self.assertIn('[host_firewall] unknown', result.stdout)

    def test_port_conflict_and_invalid_input_stop_before_writes(self):
        for options in ({'occupied': True}, {'port': '443'}, {'port': '8080'}, {'port': '65536'}, {'port': '2096:80'}, {'port': ''}):
            result, compose, config = self.run_install(**options)
            self.assertNotEqual(0, result.returncode)
            self.assertEqual('', compose)
            self.assertEqual('', config)


if __name__ == '__main__':
    unittest.main()
