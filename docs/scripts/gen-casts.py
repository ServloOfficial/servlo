#!/usr/bin/env python3
# Generates the landing-page asciinema casts. Run from docs/:
#   python3 scripts/gen-casts.py
# Heights are sized to each cast's content so the terminal windows hug it.
import json, os

OUT = 'public/casts'
E = '\x1b'
C = dict(rst=E+'[0m', dim=E+'[38;5;245m', prompt=E+'[38;5;210m', cmd=E+'[38;5;255m',
         ok=E+'[38;5;42m', url=E+'[38;5;39m', vio=E+'[38;5;141m', amb=E+'[38;5;215m',
         pink=E+'[38;5;211m', bold=E+'[1m')


def col(name, s):
    return C[name] + s + C['rst']


class Cast:
    def __init__(self, w, h):
        self.w = w
        self.h = h
        self.t = 0.0
        self.ev = []

    def out(self, s):
        self.ev.append([round(self.t, 3), 'o', s])

    def wait(self, dt):
        self.t += dt

    def type(self, prompt, cmd):
        self.out(col('prompt', prompt))
        for ch in cmd:
            self.wait(0.045)
            self.out(col('cmd', ch))
        self.wait(0.35)
        self.out('\r\n')

    def line(self, s='', dt=0.45):
        self.wait(dt)
        self.out(s + '\r\n')

    def write(self, name):
        header = {'version': 2, 'width': self.w, 'height': self.h,
                  'theme': {'fg': '#f4f4f5', 'bg': '#0d0d0d',
                            'palette': '#0d0d0d:#ff5b50:#34d399:#fbbf24:#38bdf8:#a78bfa:#38bdf8:#c4c4cc'}}
        with open(os.path.join(OUT, name), 'w') as f:
            f.write(json.dumps(header) + '\n')
            for e in self.ev:
                f.write(json.dumps(e) + '\n')


os.makedirs(OUT, exist_ok=True)

# ---- hero (11 content lines) ----
c = Cast(74, 12)
c.type('servlo@droplet $ ', 'servlo apps install wordpress --domain blog.example.com')
c.line(col('dim', '→ site · ') + col('ok', 'blog.example.com') + col('dim', ' · php ') + col('vio', '8.4') + col('dim', ' · own FPM pool'))
c.line(col('dim', '→ database · ') + col('ok', 'blog_example_com') + col('dim', ' · least-privilege user'))
c.line(col('dim', '→ nginx vhost written · ') + col('ok', 'nginx -t passed'))
c.line(col('ok', '✓ installed'))
c.line('')
c.type('servlo@droplet $ ', 'servlo secure blog.example.com')
c.line(col('dim', "→ dns · every domain resolves here · ") + col('ok', '✓'))
c.line(col('dim', "→ let's encrypt · http-01 … ") + col('ok', '✓ issued'))
c.line('  ' + col('dim', 'Site') + '   ' + col('url', 'https://blog.example.com'))
c.wait(3.0)
c.write('hero.cast')

# ---- quick-start: install on the droplet, then secure the site ----
s1 = Cast(70, 7)
s1.type('$ ', 'curl -fsSL https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash')
s1.line(col('dim', '→ installing podman, fpm, nginx (rootless)…'))
s1.line(col('ok', '✓ servlo ready'))
s1.wait(2.5)
s1.write('step-01.cast')

s2 = Cast(70, 7)
s2.type('servlo@droplet $ ', 'servlo secure blog.example.com')
s2.line(col('dim', '→ dns · blog.example.com resolves here · ') + col('ok', '✓'))
s2.line(col('dim', "→ let's encrypt · http-01 … ") + col('ok', '✓ issued'))
s2.line(col('dim', '→ nginx reloaded · renewal armed'))
s2.line(col('ok', '✓ live at ') + col('url', 'https://blog.example.com'))
s2.wait(3.0)
s2.write('step-02.cast')


print('casts:', sorted(os.listdir(OUT)))
