#!/usr/bin/env python3
"""Behavioral distribution tests; all filesystem/network fixtures are temporary."""
import hashlib, io, os, pathlib, shutil, stat, subprocess, tarfile, tempfile, unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "scripts"

def run(cmd, **kw): return subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, **kw)
def sha(p): return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def pkg(d):
    r=run([str(SCRIPTS/'package.sh'),'v0.1.0',str(d)]); assert r.returncode==0,r.stderr

def installer_pkg(d):
    d=pathlib.Path(d)
    payload=b'#!/bin/sh\n[ "${1:-}" = doctor ]\n'
    archive=d/'stillmac-v0.1.0-darwin-arm64.tar.gz'
    with tarfile.open(archive,'w:gz',format=tarfile.USTAR_FORMAT) as t:
        item=tarfile.TarInfo('stillmac'); item.mode=0o755; item.uid=item.gid=item.mtime=0; item.size=len(payload)
        t.addfile(item,io.BytesIO(payload))
    (d/'SHA256SUMS').write_text(f'{sha(archive)}  {archive.name}\n')

def fake_path(d, archive_dir, uname='Darwin', arch='arm64', uid=None):
    if uid is None: uid=str(os.getuid())
    b=pathlib.Path(d)/'fakebin'; b.mkdir(exist_ok=True)
    (b/'uname').write_text(f'#!/bin/sh\ncase "$1" in -s) echo {uname};; -m) echo {arch};; esac\n'); (b/'uname').chmod(0o755)
    (b/'curl').write_text('#!/bin/sh\nset -eu\nu=""; out=""; while [ "$#" -gt 0 ]; do case "$1" in -o) shift; out=$1;; http*) u=$1;; esac; shift; done\ncp "$ASSET_DIR/$(basename "$u")" "$out"\n')
    (b/'id').write_text(f'#!/bin/sh\necho {uid}\n'); (b/'id').chmod(0o755)
    (b/'curl').chmod(0o755); return b

class DistributionTests(unittest.TestCase):
 def test_activation_preserves_existing_regular_output(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); self._activation_dist(dist)
   out=p/'out'; out.write_text('keep')
   r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(out)])
   self.assertNotEqual(r.returncode,0); self.assertEqual(out.read_text(),'keep')
 def test_activation_rejects_world_writable_parent(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); self._activation_dist(dist); parent=p/'unsafe'; parent.mkdir(); os.chmod(parent,0o777)
   r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(parent/'out')])
   self.assertNotEqual(r.returncode,0); self.assertFalse((parent/'out').exists())
 def test_activation_concurrent_creation_is_exclusive(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); self._activation_dist(dist); out=p/'out'
   rs=[subprocess.Popen([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(out)],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for _ in range(12)]
   results=[]
   for x in rs:
    stdout,stderr=x.communicate(); results.append((x.returncode,stdout,stderr))
   self.assertEqual(sum(code==0 for code,_,_ in results),1); self.assertEqual(out.read_bytes(), (SCRIPTS/'install.sh.tmpl').read_bytes().replace(b'@TRUSTED_MANIFEST_SHA256@',sha(dist/'SHA256SUMS').encode()))
 def test_publisher_directory_sync_failure_removes_created_output(self):
  import importlib.util
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); template=p/'template'; template.write_text('digest=@TRUSTED_MANIFEST_SHA256@\n'); digest='a'*64
   spec=importlib.util.spec_from_file_location('publish_installer',SCRIPTS/'publish-installer.py'); module=importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
   original=module.sync_directory
   try:
    module.sync_directory=lambda _fd: (_ for _ in ()).throw(OSError('injected directory sync failure'))
    self.assertNotEqual(module.main([str(template),str(p),'out',digest]),0)
   finally:
    module.sync_directory=original
   self.assertFalse((p/'out').exists())
 def test_publisher_fchmod_failure_removes_created_output(self):
  import importlib.util
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); template=p/'template'; template.write_text('digest=@TRUSTED_MANIFEST_SHA256@\n'); digest='a'*64
   spec=importlib.util.spec_from_file_location('publish_installer_fchmod',SCRIPTS/'publish-installer.py')
   if spec is None or spec.loader is None: self.fail('cannot load publisher module')
   module=importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
   original=module.os.fchmod
   try:
    module.os.fchmod=lambda _fd,_mode: (_ for _ in ()).throw(OSError('injected fchmod failure'))
    self.assertNotEqual(module.main([str(template),str(p),'out',digest]),0)
   finally:
    module.os.fchmod=original
   self.assertFalse((p/'out').exists())

 def _activation_dist(self, dist):
  names=['stillmac-v0.1.0-darwin-arm64.tar.gz']
  for n in names: (dist/n).write_bytes(b'artifact')
  (dist/'SHA256SUMS').write_text(''.join(f'{sha(dist/n)}  {n}\n' for n in names))
 def test_package_build_disables_vcs_metadata(self):
  self.assertIn('go build -buildvcs=false -trimpath', (SCRIPTS/'package.sh').read_text())
 def test_activation_script_syntax_and_success(self):
  self.assertEqual(run(['sh','-n',str(SCRIPTS/'activate-installer.sh')]).returncode,0)
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir()
   names=['stillmac-v0.1.0-darwin-arm64.tar.gz']
   for i,n in enumerate(names): (dist/n).write_bytes(bytes([i+1])*17)
   (dist/'SHA256SUMS').write_text(''.join(f'{sha(dist/n)}  {n}\n' for n in names))
   out=p/'activated.sh'; r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(out)])
   self.assertEqual(r.returncode,0,r.stderr); self.assertTrue(out.stat().st_mode & 0o111)
   text=out.read_text(); self.assertNotIn('@TRUSTED_MANIFEST_SHA256@',text); self.assertEqual(text.count('TRUSTED_MANIFEST_SHA256='),1)
 def test_activation_default_output_accepts_relative_dist(self):
  with tempfile.TemporaryDirectory(dir=ROOT) as d:
   dist=pathlib.Path(d); self._activation_dist(dist)
   relative_dist=os.path.relpath(dist,ROOT)
   r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',relative_dist])
   self.assertEqual(r.returncode,0,r.stderr)
   self.assertTrue((dist/'stillmac-install-v0.1.0.sh').is_file())
 def test_activation_rejects_relative_symlink_dist(self):
  with tempfile.TemporaryDirectory(dir=ROOT) as d:
   base=pathlib.Path(d); dist=base/'dist'; dist.mkdir(); self._activation_dist(dist)
   link=base/'linked-dist'; link.symlink_to(dist, target_is_directory=True)
   r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',os.path.relpath(link,ROOT)])
   self.assertNotEqual(r.returncode,0)
   self.assertFalse((dist/'stillmac-install-v0.1.0.sh').exists())
 def test_activation_output_mode_is_exact_under_restrictive_umask(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); self._activation_dist(dist); out=p/'activated.sh'
   r=run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(out)],umask=0o077)
   self.assertEqual(r.returncode,0,r.stderr)
   self.assertEqual(stat.S_IMODE(out.stat().st_mode),0o755)
 def test_activation_rejects_tampered_or_malformed_distribution(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); names=['stillmac-v0.1.0-darwin-arm64.tar.gz']
   for n in names: (dist/n).write_bytes(b'artifact')
   (dist/'SHA256SUMS').write_text('0'*64+'  '+names[0]+'\\n'+'0'*64+'  '+names[0]+'\\n')
   self.assertNotEqual(run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(p/'out')]).returncode,0)
 def test_activation_refuses_symlink_output(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); dist=p/'dist'; dist.mkdir(); names=['stillmac-v0.1.0-darwin-arm64.tar.gz']
   for n in names: (dist/n).write_bytes(b'x')
   (dist/'SHA256SUMS').write_text(''.join(f'{sha(dist/n)}  {n}\n' for n in names)); target=p/'target'; target.write_text('x'); out=p/'out'; out.symlink_to(target)
   self.assertNotEqual(run([str(SCRIPTS/'activate-installer.sh'),'v0.1.0',str(dist),str(out)]).returncode,0)
 def test_version_rejects_evil(self): self.assertNotEqual(run([str(SCRIPTS/'package.sh'),'v1.2.3evil',str(ROOT/'tmp-out')]).returncode,0)
 def test_version_requires_three_numeric_components(self):
  for v in ('1.2.3','v1.2','v1..3','v01.2.3','v1.2.3-rc1'): self.assertNotEqual(run([str(SCRIPTS/'package.sh'),v,'/tmp/stillmac-version']).returncode,0)
 def test_output_symlink_refused(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); target=p/'x'; target.mkdir(); out=p/'out'; out.symlink_to(target); self.assertNotEqual(run([str(SCRIPTS/'package.sh'),'v1.2.3',str(out)]).returncode,0)
 def test_output_preserves_unrelated(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'keep').write_text('keep'); pkg(p); self.assertEqual((p/'keep').read_text(),'keep')
 def test_package_names_and_manifest(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p); self.assertEqual(sorted(x.name for x in p.iterdir()),['SHA256SUMS','stillmac-v0.1.0-darwin-arm64.tar.gz']); self.assertEqual(len((p/'SHA256SUMS').read_text().splitlines()),1)
 def test_package_manifest_checksums_verify(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p)
   for line in (p/'SHA256SUMS').read_text().splitlines(): h,n=line.split(); self.assertEqual(h,sha(p/n))
 def test_package_archive_metadata_exact(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p)
   with tarfile.open(next(p.glob('*.tar.gz'))) as t:
    i=t.getmember('stillmac'); self.assertEqual((i.name,i.type,i.mode,i.uid,i.gid,i.mtime),('stillmac',tarfile.REGTYPE,0o755,0,0,0))
 def test_package_is_byte_deterministic(self):
  with tempfile.TemporaryDirectory() as d:
   a=pathlib.Path(d)/'a'; b=pathlib.Path(d)/'b'; a.mkdir(); b.mkdir(); pkg(a); pkg(b)
   for n in ('SHA256SUMS','stillmac-v0.1.0-darwin-arm64.tar.gz'): self.assertEqual((a/n).read_bytes(),(b/n).read_bytes())
 def test_installer_no_python_or_bypass(self):
  s=(SCRIPTS/'install.sh').read_text(); self.assertNotIn('python3',s); self.assertNotIn('--test-local',s); self.assertIn('fail-closed',s)
 def installer(self, d, mutate=None, old=None, extra_manifest=None, archive=None, uid=None):
  if uid is None: uid=str(os.getuid())
  p=pathlib.Path(d); assets=p/'assets'; assets.mkdir(exist_ok=True)
  if not (assets/'SHA256SUMS').exists(): installer_pkg(assets)
  if archive: shutil.copy2(archive,assets/'stillmac-v0.1.0-darwin-arm64.tar.gz')
  if extra_manifest is not None: (assets/'SHA256SUMS').write_text(extra_manifest)
  home=p/'home'; home.mkdir(exist_ok=True); bindir=home/'.local'/'bin'; bindir.mkdir(parents=True,exist_ok=True); os.chmod(home,0o700); os.chmod(home/'.local',0o700); os.chmod(bindir,0o700); fb=fake_path(p,assets,uid=uid)
  activated=p/'install.sh'; manifest_hash=sha(assets/'SHA256SUMS'); template=(SCRIPTS/'install.sh.tmpl').read_text().replace('@TRUSTED_MANIFEST_SHA256@',manifest_hash); template=template.replace("PATH='/usr/bin:/bin:/usr/sbin:/sbin'",f"PATH='{fb}:/usr/bin:/bin:/usr/sbin:/sbin'"); activated.write_text(template); activated.chmod(0o755)
  env=os.environ.copy(); env.pop('EUID', None); env.update(HOME=str(home),STILLMAC_VERSION='v0.1.0',STILLMAC_DOWNLOAD_BASE='https://fake/release',ASSET_DIR=str(assets),PATH=str(fb)+':'+os.environ['PATH'])
  if mutate: mutate(p,bindir,assets)
  return run([str(activated)],env=env),bindir
 def test_generated_installer_is_standalone(self):
  with tempfile.TemporaryDirectory() as d:
   r,b=self.installer(d)
   self.assertEqual(r.returncode,0,r.stderr)
   self.assertTrue((b/'stillmac').is_file())
 def test_generated_installer_never_executes_adjacent_library(self):
  with tempfile.TemporaryDirectory() as d:
   marker=pathlib.Path(d)/'hostile-library-executed'
   hostile=pathlib.Path(d)/'lib.sh'; hostile.write_text(f'#!/bin/sh\ntouch {marker}\nexit 97\n'); hostile.chmod(0o755)
   r,b=self.installer(d)
   self.assertEqual(r.returncode,0,r.stderr)
   self.assertFalse(marker.exists())
   self.assertTrue((b/'stillmac').is_file())
 def test_installer_sets_trusted_system_path(self):
  text=(SCRIPTS/'install.sh.tmpl').read_text()
  self.assertIn("PATH='/usr/bin:/bin:/usr/sbin:/sbin'",text)
  self.assertIn('export PATH',text)
  self.assertNotIn('lib.sh',text)
  self.assertNotIn('. "$ROOT/',text)
 def test_generated_installer_ignores_hostile_inherited_path(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); assets=p/'assets'; assets.mkdir(); installer_pkg(assets)
   home=p/'home'; bindir=home/'.local'/'bin'; bindir.mkdir(parents=True)
   for directory in (home,home/'.local',bindir): os.chmod(directory,0o700)
   marker=p/'hostile-path-executed'; hostile=p/'hostile'; hostile.mkdir()
   for command in ('uname','curl','shasum','awk','tar'):
    shim=hostile/command; shim.write_text(f'#!/bin/sh\ntouch "{marker}"\nexit 99\n'); shim.chmod(0o755)
   activated=p/'install.sh'; activated.write_text((SCRIPTS/'install.sh.tmpl').read_text().replace('@TRUSTED_MANIFEST_SHA256@',sha(assets/'SHA256SUMS'))); activated.chmod(0o755)
   env=os.environ.copy(); env.update(HOME=str(home),STILLMAC_VERSION='v0.1.0',STILLMAC_DOWNLOAD_BASE=assets.as_uri(),PATH=str(hostile)+':'+os.environ['PATH'])
   r=run([str(activated)],env=env)
   self.assertEqual(r.returncode,0,r.stderr)
   self.assertFalse(marker.exists())
   self.assertTrue((bindir/'stillmac').is_file())
 def test_installer_fresh_install(self):
  with tempfile.TemporaryDirectory() as d: r,b=self.installer(d); self.assertEqual(r.returncode,0,r.stderr); self.assertTrue((b/'stillmac').is_file())
 def test_installer_idempotent_update(self):
  with tempfile.TemporaryDirectory() as d: r,b=self.installer(d); self.assertEqual(r.returncode,0); r,_=self.installer(d); self.assertEqual(r.returncode,0)
 def test_installer_checksum_mismatch(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); (p/'assets/SHA256SUMS').write_text('0'*64+'  bad.tar.gz\n'); r,b=self.installer(d); self.assertNotEqual(r.returncode,0); self.assertFalse((b/'stillmac').exists())
 def test_installer_manifest_pin_rejects_forged_archive_and_manifest(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); r,b=self.installer(d,mutate=lambda p,b,a:self._forge_assets(a)); self.assertNotEqual(r.returncode,0); self.assertFalse((b/'stillmac').exists())
 def _forge_assets(self, assets):
  archive=assets/'stillmac-v0.1.0-darwin-arm64.tar.gz'
  with tarfile.open(archive,'w:gz') as t:
   payload=b'#!/bin/sh\nexit 0\n'; item=tarfile.TarInfo('stillmac'); item.mode=0o755; item.size=len(payload); t.addfile(item,io.BytesIO(payload))
  lines=(assets/'SHA256SUMS').read_text().splitlines(); lines[0]=sha(archive)+'  '+archive.name; (assets/'SHA256SUMS').write_text('\n'.join(lines)+'\n')
 def test_installer_missing_manifest_entry(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); (p/'assets/SHA256SUMS').write_text(''); r,_=self.installer(d); self.assertNotEqual(r.returncode,0)
 def test_installer_unexpected_manifest_line(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); (p/'assets/SHA256SUMS').write_text((p/'assets/SHA256SUMS').read_text()+'\n'+'0'*64+'  extra.tar.gz\n'); r,_=self.installer(d); self.assertNotEqual(r.returncode,0)
 def test_installer_duplicate_manifest(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); m=(p/'assets/SHA256SUMS').read_text(); (p/'assets/SHA256SUMS').write_text(m+m); r,_=self.installer(d); self.assertNotEqual(r.returncode,0)
 def test_installer_unsupported_os(self):
  with tempfile.TemporaryDirectory() as d: r,_=self.installer(d, mutate=lambda p,b,a:(p/'fakebin/uname').write_text('#!/bin/sh\necho Linux\n')); self.assertNotEqual(r.returncode,0)
 def test_installer_root_refusal(self):
  with tempfile.TemporaryDirectory() as d:
   r,_=self.installer(d,uid='0'); self.assertNotEqual(r.returncode,0)
 def test_installer_bin_symlink_refusal(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); target=p/'target'; target.mkdir(); r,_=self.installer(d,mutate=lambda p,b,a:(b.rmdir() or b.symlink_to(target))); self.assertNotEqual(r.returncode,0)
 def test_installer_archive_traversal_rejected(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); bad=p/'bad.tar.gz'
   with tarfile.open(bad,'w:gz') as t: i=tarfile.TarInfo('../escape'); i.size=1; t.addfile(i,io.BytesIO(b'x'))
   r,_=self.installer(d,archive=bad); self.assertNotEqual(r.returncode,0); self.assertFalse((p/'escape').exists())
 def test_installer_rejects_archive_types_and_sets(self):
  cases=('symlink','hardlink','directory','multiple','duplicate','fifo')
  for kind in cases:
   with self.subTest(kind=kind), tempfile.TemporaryDirectory() as d:
    p=pathlib.Path(d); bad=p/'bad.tar.gz'
    with tarfile.open(bad,'w:gz',format=tarfile.USTAR_FORMAT) as t:
     if kind=='symlink':
      i=tarfile.TarInfo('stillmac'); i.type=tarfile.SYMTYPE; i.linkname='other'; t.addfile(i)
     elif kind=='hardlink':
      i=tarfile.TarInfo('stillmac'); i.type=tarfile.LNKTYPE; i.linkname='other'; t.addfile(i)
     elif kind=='directory':
      i=tarfile.TarInfo('stillmac/'); i.type=tarfile.DIRTYPE; t.addfile(i)
     else:
      i=tarfile.TarInfo('stillmac'); i.size=1; t.addfile(i,io.BytesIO(b'x'))
      if kind=='multiple':
       j=tarfile.TarInfo('other'); j.size=1; t.addfile(j,io.BytesIO(b'x'))
      elif kind=='duplicate': t.addfile(i,io.BytesIO(b'x'))
      elif kind=='fifo':
       j=tarfile.TarInfo('stillmac'); j.type=tarfile.FIFOTYPE; t.addfile(j)
    r,_=self.installer(d,archive=bad); self.assertNotEqual(r.returncode,0)
 def test_installer_doctor_failure_preserves_old(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); old=b'old-binary';
   def mutate(p,b,a):
    (b/'stillmac').write_bytes(old); bad=a/'stillmac-v0.1.0-darwin-arm64.tar.gz'
    with tarfile.open(bad,'w:gz') as t:
     payload=b'#!/bin/sh\nexit 23\n'; i=tarfile.TarInfo('stillmac'); i.mode=0o755; i.size=len(payload); t.addfile(i,io.BytesIO(payload))
    (a/'SHA256SUMS').write_text(sha(bad)+'  '+bad.name+'\n')
   r,b=self.installer(d,mutate=mutate); self.assertNotEqual(r.returncode,0); self.assertEqual((b/'stillmac').read_bytes(),old); self.assertEqual(list(b.glob('.stillmac.*')),[])
 def test_installer_rejects_intel(self):
  with tempfile.TemporaryDirectory() as d:
   r,b=self.installer(d,mutate=lambda p,b,a:(p/'fakebin/uname').write_text('#!/bin/sh\ncase "$1" in -s) echo Darwin;; -m) echo x86_64;; esac\n')); self.assertNotEqual(r.returncode,0); self.assertFalse((b/'stillmac').exists())
 def test_installer_no_data_root_on_failure(self):
  with tempfile.TemporaryDirectory() as d:
   r,_=self.installer(d,mutate=lambda p,b,a:(a/'SHA256SUMS').write_text('bad\n')); self.assertNotEqual(r.returncode,0); self.assertFalse((pathlib.Path(d)/'home/Library').exists())
 def test_installer_archive_download_failure(self):
  with tempfile.TemporaryDirectory() as d:
   r,_=self.installer(d,mutate=lambda p,b,a:(a/'stillmac-v0.1.0-darwin-arm64.tar.gz').unlink()); self.assertNotEqual(r.returncode,0)
 def test_installer_manifest_download_failure(self):
  with tempfile.TemporaryDirectory() as d:
   r,_=self.installer(d,mutate=lambda p,b,a:(a/'SHA256SUMS').unlink()); self.assertNotEqual(r.returncode,0)
 def test_installer_destination_nonregular_refusal(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); r,b=self.installer(d,mutate=lambda p,b,a:(b/'stillmac').mkdir()); self.assertNotEqual(r.returncode,0); self.assertTrue((b/'stillmac').is_dir())
 def test_package_build_failure_cleans_work(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); fake=p/'fake'; fake.mkdir(); (fake/'go').write_text('#!/bin/sh\nexit 42\n'); (fake/'go').chmod(0o755); r=run([str(SCRIPTS/'package.sh'),'v0.1.0',str(p/'dist')],env=os.environ|{'PATH':str(fake)+':'+os.environ['PATH'],'TMPDIR':str(p)}); self.assertNotEqual(r.returncode,0); self.assertEqual(list(p.glob('stillmac-package.*')),[])
 def test_uninstall_keeps_data(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); h=p/'h'; b=h/'.local'/'bin'; data=h/'Library/Application Support/StillMac'; b.mkdir(parents=True); data.mkdir(parents=True); os.chmod(h,0o700); os.chmod(h/'.local',0o700); os.chmod(b,0o700); (b/'stillmac').write_text('x'); env=os.environ|{'HOME':str(h)}; r=run([str(SCRIPTS/'uninstall.sh')],env=env); self.assertEqual(r.returncode,0,r.stderr); self.assertTrue(data.exists()); self.assertFalse((b/'stillmac').exists())
 def test_uninstall_removes_purge_option(self): self.assertNotIn('--purge-data',(SCRIPTS/'uninstall.sh').read_text())
 def test_uninstall_unknown_option_no_mutation(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); h=p/'h'; b=h/'.local'/'bin'; b.mkdir(parents=True); (b/'stillmac').write_text('x'); r=run([str(SCRIPTS/'uninstall.sh'),'--bad'],env=os.environ|{'HOME':str(h)}); self.assertNotEqual(r.returncode,0); self.assertTrue((b/'stillmac').exists())
 def test_uninstall_binary_symlink_no_mutation(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); h=p/'h'; b=h/'.local'/'bin'; b.mkdir(parents=True); (p/'x').write_text('x'); (b/'stillmac').symlink_to(p/'x'); r=run([str(SCRIPTS/'uninstall.sh')],env=os.environ|{'HOME':str(h)}); self.assertNotEqual(r.returncode,0); self.assertTrue((b/'stillmac').is_symlink())
 def test_uninstall_absent_is_idempotent(self):
  with tempfile.TemporaryDirectory() as d:
   h=pathlib.Path(d)/'h'; h.mkdir(); os.chmod(h,0o700); r=run([str(SCRIPTS/'uninstall.sh')],env=os.environ|{'HOME':str(h)}); self.assertEqual(r.returncode,0,r.stderr)
 def test_formula_template_not_installable(self): self.assertFalse((ROOT/'Formula/stillmac.rb').exists()); self.assertIn('@VERSION@',(ROOT/'Formula/stillmac.rb.tmpl').read_text())
 def test_formula_generation_and_ruby_syntax(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p); out=p/'f.rb'; r=run([str(SCRIPTS/'update-formula.sh'),'v0.1.0',str(p),str(out)]); self.assertEqual(r.returncode,0,r.stderr); self.assertEqual(run(['ruby','-c',str(out)]).returncode,0); text=out.read_text(); m={line.split()[1]:line.split()[0] for line in (p/'SHA256SUMS').read_text().splitlines()}; self.assertIn('version "0.1.0"',text); self.assertIn(m['stillmac-v0.1.0-darwin-arm64.tar.gz'],text); self.assertNotIn('on_intel',text)
 def test_formula_rejects_extra_manifest(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p); (p/'SHA256SUMS').write_text((p/'SHA256SUMS').read_text()+'0'*64+'  extra\n'); self.assertNotEqual(run([str(SCRIPTS/'update-formula.sh'),'v0.1.0',str(p),str(p/'f')]).returncode,0)
 def test_docs_remove_purge_claims(self):
  for n in ('README.md','INSTALL.md','UNINSTALL.md','docs/DISTRIBUTION-CONTRACT.md','skills/stillmac/SKILL.md'): self.assertNotIn('--purge-data',(ROOT/n).read_text())
 def test_install_docs_match_public_v011_apple_silicon_release(self):
  install=(ROOT/'INSTALL.md').read_text()
  contract=(ROOT/'docs/DISTRIBUTION-CONTRACT.md').read_text()
  for text in (install,contract): self.assertIn('Apple Silicon',text)
  self.assertIn('/releases/download/v0.1.1/stillmac-install-v0.1.1.sh',install)
  self.assertIn('## Inspect first',install)
  self.assertIn('Homebrew, INACTIVE',install)
  self.assertIn('Agent Skill, INACTIVE',install)
  self.assertNotIn('darwin-amd64',contract)
 def test_release_checklist_gates_private_prerelease_before_visibility(self):
  text=(ROOT/'docs/RELEASE-CHECKLIST.md').read_text().lower()
  self.assertIn('v0.1.0',text)
  self.assertIn('draft',text)
 def test_readme_is_a_simple_public_beta_front_page(self):
  text=(ROOT/'README.md').read_text()
  for heading in ('## Who StillMac is for','## Public beta limits','## Install','## Quick start','## Example scan','## What StillMac can change','## Process and memory snapshots','## Advanced and automated use','## Build from source','## Privacy','## Detailed documentation'):
   self.assertIn(heading,text)
  installed='$HOME/.local/bin/stillmac'
  for command in ('go build -buildvcs=false -trimpath',installed+' doctor',installed+' scan --format text',installed+' clean all',installed+' explain',installed+' plan',installed+' apply'):
   self.assertIn(command,text)
  self.assertIn('Mac developers',text)
  self.assertIn('Signing and Apple notarisation are not claimed for this beta.',text)
  self.assertIn('Example only. Your sizes and IDs will differ.',text)
  self.assertIn('`sample` saves one point-in-time process and memory snapshot.',text)
  self.assertLess(text.index('## Quick start'),text.index('## Build from source'))
  self.assertLess(text.index(installed+' clean all'),text.index(installed+' plan'))
  self.assertNotIn('./bin/stillmac',text[:text.index('## Build from source')])
  self.assertIn("StillMac's first public beta release is `v0.1.1`.",text)
  self.assertIn('Apple Silicon Macs only',text)
  self.assertIn('curl -fsSL https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.1/stillmac-install-v0.1.1.sh | sh',text)
  self.assertNotIn('brew install HYPHNLabs/tap/stillmac',text)
  self.assertNotIn('npx skills add HYPHNLabs/StillMac -g',text)
 def test_public_scope_ownership_and_security_route_are_explicit(self):
  self.assertNotIn('amd64',(SCRIPTS/'package.sh').read_text())
  self.assertNotIn('x86_64',(SCRIPTS/'install.sh.tmpl').read_text())
  self.assertIn('contact@hyphnlabs.com',(ROOT/'SECURITY.md').read_text())
  self.assertNotIn('Distribution remains inactive.',(ROOT/'THREAT-MODEL.md').read_text())
  self.assertNotIn('inactive release installer template',(ROOT/'PRIVACY.md').read_text())
  self.assertIn('Copyright 2026 HYPHN Labs',(ROOT/'NOTICE').read_text())
  self.assertNotIn('precise citation',(ROOT/'ACKNOWLEDGEMENTS.md').read_text().lower())
 def test_readme_describes_current_cleanup_boundary_and_approval_flow(self):
  text=(ROOT/'README.md').read_text()
  for claim in ('exact Go build cache','Homebrew caches, Codex runtimes, and Git worktrees','inventory-only','15 minutes','explicit approval','revalidation','receipt'):
   self.assertIn(claim,text)
  self.assertIn('go clean -cache',text)
  self.assertIn('No telemetry',text)
  self.assertIn('Local',text)
  self.assertIn('Process and memory samples exclude command arguments, environment variables, full executable paths',text)
  self.assertIn('Cleanup private state retains the exact action target, host binding, protection and plan records, receipts, and verified Go executable identity required to revalidate an approved plan.',text)
  self.assertIn('Public output excludes the private target paths and executable identity.',text)
  self.assertNotIn('No collection of command arguments, environment variables, full executable paths',text)
 def test_cleanup_contract_and_skill_enforce_approval_flow(self):
  contract=(ROOT/'docs/DEVELOPER-CLEANUP-CONTRACT.md').read_text()
  skill=(ROOT/'skills/stillmac/SKILL.md').read_text()
  for token in ('all-safe','15 minutes','target registry','BLOCKED_CHANGED','reclaimed_bytes','owner-native-go-clean-cache','owner_action_failed'):
   self.assertIn(token,contract)
  for step in ('scan','list','choice','plan','approval','apply'):
   self.assertIn(step,skill.lower())
  self.assertIn('never invent',skill.lower())
  self.assertIn('scan-only',skill.lower())
 def test_cleanup_docs_match_owner_native_boundary_and_go_123_requirement(self):
  removed_feature='quaran'+'tine'
  for name in ('AGENTS.md','README.md','PRIVACY.md','SECURITY.md','THREAT-MODEL.md','ROADMAP.md','CHANGELOG.md','UNINSTALL.md','docs/ARCHITECTURE.md','docs/DATA-LOCATIONS.md','docs/DEVELOPER-CLEANUP-CONTRACT.md','docs/V0.1-TRACER-CONTRACT.md','skills/stillmac/SKILL.md'):
   text=(ROOT/name).read_text().lower()
   self.assertNotIn(removed_feature,text,name)
  self.assertEqual((ROOT/'go.mod').read_text().splitlines()[2],'go 1.23')
  self.assertNotIn('Go 1.'+'25',(ROOT/'docs/DEVELOPMENT.md').read_text())
if __name__=='__main__': unittest.main(verbosity=2)
