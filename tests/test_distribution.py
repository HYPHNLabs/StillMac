#!/usr/bin/env python3
"""Behavioral distribution tests; all filesystem/network fixtures are temporary."""
import hashlib, io, os, pathlib, shutil, stat, subprocess, tarfile, tempfile, unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "scripts"

def run(cmd, **kw): return subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, **kw)
def sha(p): return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def pkg(d):
    r=run([str(SCRIPTS/'package.sh'),'v0.1.0',str(d)]); assert r.returncode==0,r.stderr

def fake_path(d, archive_dir, uname='Darwin', arch='arm64', uid=None):
    if uid is None: uid=str(os.getuid())
    b=pathlib.Path(d)/'fakebin'; b.mkdir(exist_ok=True)
    (b/'uname').write_text(f'#!/bin/sh\ncase "$1" in -s) echo {uname};; -m) echo {arch};; esac\n'); (b/'uname').chmod(0o755)
    (b/'curl').write_text('#!/bin/sh\nset -eu\nu=""; out=""; while [ "$#" -gt 0 ]; do case "$1" in -o) shift; out=$1;; http*) u=$1;; esac; shift; done\ncp "$ASSET_DIR/$(basename "$u")" "$out"\n')
    (b/'id').write_text(f'#!/bin/sh\necho {uid}\n'); (b/'id').chmod(0o755)
    (b/'curl').chmod(0o755); return b

class DistributionTests(unittest.TestCase):
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
   p=pathlib.Path(d); pkg(p); self.assertEqual(sorted(x.name for x in p.iterdir()),['SHA256SUMS','stillmac-v0.1.0-darwin-amd64.tar.gz','stillmac-v0.1.0-darwin-arm64.tar.gz']); self.assertEqual(len((p/'SHA256SUMS').read_text().splitlines()),2)
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
   for n in ('SHA256SUMS','stillmac-v0.1.0-darwin-arm64.tar.gz','stillmac-v0.1.0-darwin-amd64.tar.gz'): self.assertEqual((a/n).read_bytes(),(b/n).read_bytes())
 def test_installer_no_python_or_bypass(self):
  s=(SCRIPTS/'install.sh').read_text(); self.assertNotIn('python3',s); self.assertNotIn('--test-local',s); self.assertIn('fail-closed',s)
 def installer(self, d, mutate=None, old=None, extra_manifest=None, archive=None, uid=None):
  if uid is None: uid=str(os.getuid())
  p=pathlib.Path(d); assets=p/'assets'; assets.mkdir(exist_ok=True)
  if not (assets/'SHA256SUMS').exists(): pkg(assets)
  if archive: shutil.copy2(archive,assets/'stillmac-v0.1.0-darwin-arm64.tar.gz')
  if extra_manifest is not None: (assets/'SHA256SUMS').write_text(extra_manifest)
  home=p/'home'; home.mkdir(exist_ok=True); bindir=home/'.local'/'bin'; bindir.mkdir(parents=True,exist_ok=True); os.chmod(home,0o700); os.chmod(home/'.local',0o700); os.chmod(bindir,0o700); fb=fake_path(p,assets,uid=uid)
  activated=p/'install.sh'; manifest_hash=sha(assets/'SHA256SUMS'); activated.write_text((SCRIPTS/'install.sh.tmpl').read_text().replace('@TRUSTED_MANIFEST_SHA256@',manifest_hash).replace('ROOT=$(CDPATH= cd -- "$(dirname "$0")" && pwd)\n. "$ROOT/lib.sh"','ROOT='+str(SCRIPTS)+'\n. "$ROOT/lib.sh"')); activated.chmod(0o755)
  env=os.environ.copy(); env.pop('EUID', None); env.update(HOME=str(home),STILLMAC_DOWNLOAD_BASE='https://fake/release',ASSET_DIR=str(assets),PATH=str(fb)+':'+os.environ['PATH'])
  if mutate: mutate(p,bindir,assets)
  return run([str(activated)],env=env),bindir
 def test_installer_fresh_install(self):
  with tempfile.TemporaryDirectory() as d: r,b=self.installer(d); self.assertEqual(r.returncode,0,r.stderr); self.assertTrue((b/'stillmac').is_file())
 def test_installer_idempotent_update(self):
  with tempfile.TemporaryDirectory() as d: r,b=self.installer(d); self.assertEqual(r.returncode,0); r,_=self.installer(d); self.assertEqual(r.returncode,0)
 def test_installer_checksum_mismatch(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); (p/'assets/SHA256SUMS').write_text('0'*64+'  bad.tar.gz\n'+'0'*64+'  other.tar.gz\n'); r,b=self.installer(d); self.assertNotEqual(r.returncode,0); self.assertFalse((b/'stillmac').exists())
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
   p=pathlib.Path(d); (p/'assets').mkdir(); pkg(p/'assets'); lines=(p/'assets/SHA256SUMS').read_text().splitlines(); (p/'assets/SHA256SUMS').write_text(lines[0]+'\n'); r,_=self.installer(d); self.assertNotEqual(r.returncode,0)
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
    (a/'SHA256SUMS').write_text(sha(bad)+'  '+bad.name+'\n'+'0'*64+'  stillmac-v0.1.0-darwin-amd64.tar.gz\n')
   r,b=self.installer(d,mutate=mutate); self.assertNotEqual(r.returncode,0); self.assertEqual((b/'stillmac').read_bytes(),old); self.assertEqual(list(b.glob('.stillmac.*')),[])
 def test_installer_selects_amd64(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); assets=p/'assets'; assets.mkdir(); pkg(assets); (assets/'stillmac-v0.1.0-darwin-arm64.tar.gz').unlink()
   r,b=self.installer(d,mutate=lambda p,b,a:(p/'fakebin/uname').write_text('#!/bin/sh\ncase "$1" in -s) echo Darwin;; -m) echo x86_64;; esac\n')); self.assertEqual(r.returncode,0,r.stderr); self.assertTrue((b/'stillmac').exists())
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
   p=pathlib.Path(d); pkg(p); out=p/'f.rb'; r=run([str(SCRIPTS/'update-formula.sh'),'v0.1.0',str(p),str(out)]); self.assertEqual(r.returncode,0,r.stderr); self.assertEqual(run(['ruby','-c',str(out)]).returncode,0); text=out.read_text(); m={line.split()[1]:line.split()[0] for line in (p/'SHA256SUMS').read_text().splitlines()}; self.assertIn('version "0.1.0"',text); self.assertIn(m['stillmac-v0.1.0-darwin-arm64.tar.gz'],text); self.assertIn(m['stillmac-v0.1.0-darwin-amd64.tar.gz'],text)
 def test_formula_rejects_extra_manifest(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d); pkg(p); (p/'SHA256SUMS').write_text((p/'SHA256SUMS').read_text()+'0'*64+'  extra\n'); self.assertNotEqual(run([str(SCRIPTS/'update-formula.sh'),'v0.1.0',str(p),str(p/'f')]).returncode,0)
 def test_docs_remove_purge_claims(self):
  for n in ('README.md','INSTALL.md','UNINSTALL.md','docs/DISTRIBUTION-CONTRACT.md','skills/stillmac/SKILL.md'): self.assertNotIn('--purge-data',(ROOT/n).read_text())
 def test_readme_documents_only_real_cli_commands_and_inactive_install_routes(self):
  text=(ROOT/'README.md').read_text()
  for command in ('stillmac doctor','stillmac sample','stillmac status','stillmac report','stillmac report --format json','stillmac report --format markdown','stillmac help'):
   self.assertIn(command,text)
  self.assertIn('There is no `stillmac learn` command',text)
  self.assertIn('brew install HYPHNLabs/tap/stillmac',text)
  self.assertIn('npx skills add HYPHNLabs/StillMac -g',text)
  self.assertIn('**neither is active yet**',text)
  self.assertLess(text.index('## One-line installation routes'),text.index('## Quick start'))
  brew=text.index('```bash\nbrew install HYPHNLabs/tap/stillmac\n```')
  skill=text.index('```bash\nnpx skills add HYPHNLabs/StillMac -g\n```')
  self.assertLess(brew,skill)
 def test_readme_separates_cleanup_vision_from_current_read_only_beta(self):
  text=(ROOT/'README.md').read_text()
  self.assertIn('StillMac learns what is normal on your Mac',text)
  for label in ('CURRENT — READ-ONLY','COMING SOON — APPROVAL-GATED','SAFETY GATE — CLEANUP IS NOT ENABLED IN THE CURRENT BETA','caches · stale worktrees','quarantine · rollback'):
   self.assertIn(label,text)
  self.assertNotIn('stillmac-workflow.svg',text)
  self.assertFalse((ROOT/'docs/assets/stillmac-workflow.svg').exists())
  self.assertIn('not implemented in the current beta',text)
  self.assertLess(text.index('## How StillMac works'),text.index('## Release state'))
if __name__=='__main__': unittest.main(verbosity=2)
