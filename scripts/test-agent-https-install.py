#!/usr/bin/env python3
"""Exercise the real installer with isolated files and fake Docker/host commands."""
import fcntl
import itertools
import json
import os
from pathlib import Path
import shutil
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
        self.assertEqual([str(self.module.UPDATER_WORKER), 'docker-updater', 'setup', '--dir', str(self.directory)], self.calls[-1])

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


class NewInstall(unittest.TestCase):
    def run_install(self, proxy=True, host=False, occupied=False, port='2096', container_proxy=False):
        with tempfile.TemporaryDirectory(prefix='captain-new-install-') as directory:
            root = Path(directory)
            bin_dir = root / 'bin'
            bin_dir.mkdir()
            fake = bin_dir / 'host-fake'
            fake.write_text('''#!/usr/bin/env python3
import os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
if name=='uname': print('Linux' if args==['-s'] else 'x86_64')
elif name=='id': print('0')
elif name=='ss':
    if os.environ.get('OCCUPIED')=='1' and 'sport = :2096' in args: print('LISTEN fixture')
    elif os.environ.get('CONTAINER_PROXY')=='1' and 'sport = :80' in args: print('LISTEN users:(("docker-proxy",pid=123,fd=1))')
elif name=='ps': print('docker-proxy')
elif name=='docker':
    if args[:1]==['ps'] and 'publish=80' in args: print('openresty')
    elif args[:1]==['inspect']:
        print('proxy-net' if 'range' in args[2] else 'bridge')
    elif args[:2]==['network','inspect']: print('10.10.0.2')
    elif args[:1]==['version']: print('27.0.0')
elif name=='sed':
    if args[:1]==['-i']:
        path=pathlib.Path(args[-1]); path.write_text(''.join(line for line in path.read_text().splitlines(True) if line.strip()))
    else: os.execv(os.environ['REAL_SED'], ['sed']+args)
''')
            fake.chmod(0o755)
            for name in ('uname', 'id', 'ss', 'ps', 'docker', 'ip', 'chown', 'sleep', 'sed'):
                (bin_dir / name).symlink_to(fake)
            script = root / 'install.sh'
            # All installation paths are relocated before execution; no root or Docker operations occur.
            script.write_text(INSTALLER.replace('/opt/captain', str(root / 'app')).replace('/etc/captain', str(root / 'etc')))
            args = ['sh', str(script), '--mode', 'docker', '--domain', 'panel.example.com', '--email', 'admin@example.com', '--admin-email', 'admin@example.com', '--admin-password', 'fixture-password', '--agent-https-port', port]
            if proxy:
                args.append('--behind-proxy')
            if host:
                args.extend(['--network', 'host'])
            env = dict(os.environ, PATH=str(bin_dir) + ':' + os.environ['PATH'], REAL_SED=shutil.which('sed'), OCCUPIED='1' if occupied else '0', CONTAINER_PROXY='1' if container_proxy else '0')
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
                if container_proxy:
                    self.assertIn('networks: [default, proxy-net]', compose)

    def test_host_and_host_with_proxy(self):
        for container_proxy in (False, True):
            result, compose, _ = self.run_install(host=True, container_proxy=container_proxy)
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn('network_mode: host', compose)
            self.assertNotIn('ports:', compose)

    def test_port_conflict_and_invalid_input_stop_before_writes(self):
        for options in ({'occupied': True}, {'port': '443'}, {'port': '8080'}, {'port': '65536'}, {'port': '2096:80'}, {'port': ''}):
            result, compose, config = self.run_install(**options)
            self.assertNotEqual(0, result.returncode)
            self.assertEqual('', compose)
            self.assertEqual('', config)


if __name__ == '__main__':
    unittest.main()
