"""Draw the phase-review illustrations as SVG files and their Markdown summaries.

Each figure in docs/FIGURES.md sits between matching review-figure
comment markers. This script redraws docs/assets/reviews/<name>.svg and the
visual-summary block (image, what to watch, numbered steps) inside each marker.
The surrounding historical prose is not generated or rewritten.

    python3 scripts/docs/review_figures.py          # rewrite figures
    python3 scripts/docs/review_figures.py --check  # fail if any are stale
"""
from html import escape
from pathlib import Path
import re
import sys
from figure_measurements import LOCAL_SAMPLES, UPLOAD_WAIT_SAMPLE, CARGO_SAMPLES, CARGO_WORKLOADS
import statistics

ROOT = Path(__file__).resolve().parents[2] / 'docs'
INK = '#24394b'
BLUE = '#236ba0'
GREEN = '#167767'
RED = '#ac3c43'
GOLD = '#97650d'


class Drawing:
    def __init__(self, name, height, description, measured=False):
        self.name = name
        self.height = height
        self.parts = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 720 {height}" role="img" aria-labelledby="{name}-title {name}-desc">',
                      f'<title id="{name}-title">{escape(description)}</title>',
                      f'<desc id="{name}-desc">' + ('Measured local observations, not a general performance guarantee.' if measured else 'Illustrative records and execution order, not measured timings.') + ' Read the text explanation below for details.</desc>',
                      '<rect width="720" height="100%" rx="12" fill="#fcfbf7"/>']

    def text(self, x, y, value, size=16, color=INK, anchor='start'):
        self.parts.append(f'<text x="{x}" y="{y}" font-family="system-ui, sans-serif" font-size="{size}" fill="{color}" text-anchor="{anchor}">{escape(value)}</text>')

    def rect(self, x, y, w, h, fill='#e8f2f5', stroke=BLUE, radius=6):
        self.parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" stroke="{stroke}"/>')

    def line(self, x, y, xx, yy, color=BLUE, dashed=False, arrow=False):
        dash = ' stroke-dasharray="5 6"' if dashed else ''
        self.parts.append(f'<path d="M{x} {y} L{xx} {yy}" fill="none" stroke="{color}" stroke-width="2"{dash}/>')
        if arrow:
            import math
            angle = math.atan2(yy-y, xx-x)
            a = (xx-9*math.cos(angle-.45), yy-9*math.sin(angle-.45))
            b = (xx-9*math.cos(angle+.45), yy-9*math.sin(angle+.45))
            self.parts.append(f'<path d="M{a[0]:.1f} {a[1]:.1f} L{xx} {yy} L{b[0]:.1f} {b[1]:.1f}" fill="none" stroke="{color}" stroke-width="2"/>')

    def dot(self, x, y, label, color=GREEN):
        self.parts.append(f'<circle cx="{x}" cy="{y}" r="20" fill="{color}"/>')
        self.text(x, y+6, label, 16, '#ffffff', 'middle')

    def file(self, x, y, label, color=GREEN):
        self.parts.append(f'<path d="M{x} {y} h44 l14 14 v62 h-58 Z M{x+44} {y} v14 h14" fill="#ffffff" stroke="{color}" stroke-width="2"/>')
        self.text(x+29, y+47, label, 18, color, 'middle')

    def step(self, n):
        self.parts.append(f'<g data-step="{n}">')

    def end(self):
        self.parts.append('</g>')

    def finish(self):
        return '\n'.join(self.parts + ['</svg>'])


def manifest():
    d = Drawing('manifest', 390, 'A manifest groups fields; two fields contain sequences')
    d.rect(25, 30, 260, 320, '#ffffff', INK)
    d.text(45, 60, 'Manifest', 22)
    for i, (field, value) in enumerate([('Directory', '/work/books'), ('Platform', 'darwin/arm64'), ('Command', 'ordered arguments →'), ('Inputs', 'input records →')]):
        y = 100+i*66
        d.text(45, y, field, 13, BLUE)
        d.text(45, y+24, value)
    d.line(285, 246, 342, 76, arrow=True)
    for i, word in enumerate(['uv', 'run', 'pytest']):
        x = 355+i*108
        d.rect(x, 48, 100, 50)
        d.text(x+50, 80, word, 18, INK, 'middle')
        d.text(x+50, 125, f'argument {i}', 12, BLUE, 'middle')
    d.line(285, 312, 342, 222, arrow=True)
    for y, kind, name, digest in [(172, 'file', 'tests/test_books.py', 'A'), (267, 'environment', 'LANG', 'B')]:
        d.rect(355, y, 316, 78, '#edf5ef', GREEN)
        d.text(370, y+23, kind, 13, GREEN)
        d.text(370, y+47, name)
        d.text(370, y+67, f'hash: {digest}', 13)
    d.text(360, 374, 'Each input has the same three fields.', 14)
    return d.finish(), [], 'Follow the two arrows: Command is a sequence of words; Inputs is a sequence of records. This shows selected fields, not a memory-address layout.'


def key_change():
    d = Drawing('keys', 350, 'Editing recorded file bytes changes the lookup key in this example')
    for y, version, digest, key, color in [(80, 'original bytes', 'A', 'K1', BLUE), (235, 'edited bytes', 'B', 'K2', GREEN)]:
        d.text(25, y-30, 'BEFORE' if y == 80 else 'AFTER', 12, color)
        d.file(28, y, digest, color)
        d.text(110, y+20, version, 16, color)
        d.line(110, y+42, 285, y+42, color, arrow=True)
        d.rect(300, y, 180, 76, '#ffffff', color)
        d.text(390, y+30, 'Manifest', 18, color, 'middle')
        d.text(390, y+55, f'file hash = {digest}', 14, color, 'middle')
        d.line(480, y+38, 564, y+38, color, arrow=True)
        d.dot(608, y+38, key, color)
    d.line(390, 158, 390, 217, GOLD, dashed=True)
    d.text(418, 194, 'only this field changes', 13, GOLD)
    return d.finish(), [], 'Trace each row from left to right. The command stays the same; the recorded file hash and lookup key change. Matching keys still say nothing about unrecorded website responses.'


def local_flow():
    d = Drawing('local-flow', 470, 'A cache hit replays output; a miss runs the command and checks inputs again')
    d.rect(205, 25, 310, 52, '#ffffff', BLUE)
    d.text(360, 57, 'Describe inputs → look up key', 17, BLUE, 'middle')
    d.line(280, 77, 155, 144, GREEN, arrow=True)
    d.line(440, 77, 562, 144, GOLD, arrow=True)
    d.text(160, 108, 'usable hit', 14, GREEN)
    d.text(490, 108, 'miss', 14, GOLD)
    d.rect(30, 152, 255, 62, '#e3f1eb', GREEN)
    d.text(157, 189, 'Print saved output', 18, GREEN, 'middle')
    d.rect(435, 152, 255, 62, '#fff2d6', GOLD)
    d.text(562, 189, 'Run pytest', 18, GOLD, 'middle')
    d.line(562, 215, 562, 267, GOLD, arrow=True)
    d.rect(415, 275, 280, 66, '#ffffff', BLUE)
    d.text(555, 303, 'Describe inputs again', 17, BLUE, 'middle')
    d.text(555, 326, 'same? successful? fits?', 14, BLUE, 'middle')
    d.line(555, 342, 555, 388, GREEN, arrow=True)
    d.text(580, 374, 'eligible', 13, GREEN)
    d.rect(415, 396, 280, 48, '#e3f1eb', GREEN)
    d.text(555, 425, 'Save for another run', 17, GREEN, 'middle')
    d.line(415, 308, 294, 308, RED, arrow=True)
    d.text(45, 305, 'Otherwise: do not save.', 16, RED)
    d.text(45, 332, 'The command still ran.', 15)
    d.text(32, 447, 'A hit does not run pytest.', 17, GREEN)
    return d.finish(), [], 'Follow the split. A hit skips command execution, not input discovery. A miss runs the command and checks its inputs again before deciding whether to save. This is the ordinary local path; verification and quarantine add further decisions.'


def admission():
    d = Drawing('admission', 390, 'A concurrency limit admits work only when a slot is free')
    d.text(25, 35, 'FOUR SLOTS DRAWN FOR TEACHING', 14)
    for i in range(4):
        d.rect(125+i*140, 110, 104, 72, '#dfedf7', BLUE)
        d.text(177+i*140, 153, f'job {i+1}', 17, BLUE, 'middle')
    d.text(25, 85, 'All occupied', 15, BLUE)
    d.line(66, 231, 590, 231, RED, arrow=True)
    d.text(25, 263, 'new request', 15, RED)
    d.rect(595, 206, 96, 50, '#ffffff', RED)
    d.text(643, 238, '503', 20, RED, 'middle')
    d.text(290, 219, 'no free slot → no waiting queue', 14, RED, 'middle')
    d.line(597, 184, 597, 309, GREEN, dashed=True, arrow=True)
    d.text(25, 330, 'When a job finishes, it releases its slot.', 17, GREEN)
    d.text(25, 360, 'A later request can enter. This limits overlap, not requests per second.', 16)
    return d.finish(), [], 'Count occupied slots. The actual default storage admission limit is 16; four are drawn to keep the mechanism visible. Being admitted does not mean a request is already using a database connection or holding a storage lock.'


def storage():
    d = Drawing('storage', 395, 'Two result records share one output file X')
    d.text(30, 35, 'POSTGRESQL: descriptions', 14, BLUE)
    d.text(458, 35, 'FILESYSTEM: output bytes', 14, GREEN)
    for y, key in [(80, 'K1'), (240, 'K2')]:
        d.rect(30, y, 220, 104, '#ffffff', BLUE)
        d.text(48, y+29, f'Result {key}', 20)
        d.text(48, y+58, 'stdout → X', 16, BLUE)
        d.text(48, y+83, 'stderr → Y', 16, GREEN)
        d.line(250, y+53, 485, 129, BLUE, arrow=True)
        d.line(250, y+80, 485, 284, GREEN, arrow=True)
    d.file(504, 91, 'X', BLUE)
    d.text(584, 133, '“tests passed”', 14)
    d.file(504, 246, 'Y', GREEN)
    d.text(584, 288, 'empty stderr', 14)
    d.text(32, 379, 'Two references to X do not require two copies of X.', 17)
    return d.finish(), [], 'Follow both blue arrows. Different input keys can point to the same output bytes. The server reads a description, follows its hashes to files, and sends the outputs back; it does not run pytest.'


def interrupted():
    d = Drawing('interrupted', 420, 'A failed database publication leaves files for later cleanup')
    for x, label in [(110, 'Writer'), (355, 'Output files'), (610, 'Database')]:
        d.text(x, 35, label, 17, INK, 'middle')
        d.line(x, 55, x, 355, '#b9c5c8', dashed=True)
    d.step(1)
    d.line(110, 100, 355, 100, GREEN, arrow=True)
    d.text(215, 85, 'save X and Y', 14, GREEN, 'middle')
    d.end()
    d.step(2)
    d.line(110, 188, 610, 188, BLUE, arrow=True)
    d.text(365, 169, 'try to publish their references', 14, BLUE, 'middle')
    d.dot(610, 225, '×', RED)
    d.text(450, 263, 'fails before commit', 15, RED)
    d.end()
    d.step(3)
    d.file(325, 282, 'X')
    d.file(395, 282, 'Y')
    d.text(40, 390, 'Files remain. No committed result points to them.', 18)
    d.end()
    return d.finish(), ['The writer saves complete output files first.', 'In this example, database publication definitely fails before commit.', 'Rolling back SQL does not remove files. Later collection can find them.'], 'Step through the two different stores. Watch the files remain after the database step fails. This example is not an uncertain commit acknowledgement.'


def locks():
    d = Drawing('locks', 425, 'Shared reader locks make cleanup wait before deleting output')
    d.text(25, 32, 'Execution order → (spacing is not elapsed time)', 14)
    for y, label in [(90, 'Reader A'), (160, 'Reader B'), (260, 'Cleanup')]:
        d.text(20, y+8, label, 16)
        d.line(135, y, 685, y, '#c8d1d4')
    d.step(1)
    d.rect(150, 68, 265, 44, '#dfedf7', BLUE)
    d.text(165, 96, 'record → open X → read X', 14, BLUE)
    d.rect(225, 138, 190, 44, '#dfedf7', BLUE)
    d.text(242, 166, 'another reader', 14, BLUE)
    d.end()
    d.step(2)
    d.rect(245, 238, 170, 44, '#fff2d6', GOLD)
    d.text(330, 266, 'WAIT', 16, GOLD, 'middle')
    d.line(415, 50, 415, 306, GOLD, dashed=True)
    d.text(420, 327, 'readers release locks', 14, GOLD, 'middle')
    d.end()
    d.step(3)
    d.rect(430, 238, 245, 44, '#e3f1eb', GREEN)
    d.text(552, 266, 'cleanup can now proceed', 14, GREEN, 'middle')
    d.text(25, 377, 'The protected interval includes opening the file—not just reading SQL.', 16)
    d.end()
    return d.finish(), ['Two readers can hold shared locks together.', 'Cleanup needs exclusive access. It waits for both readers to finish.', 'After the readers release their locks, cleanup can proceed; readers and writers wait while it holds exclusive access.'], 'Read across each lane. The amber interval is waiting, not cleanup work. Without coordination, retention and collection could remove X after a reader found its description but before it opened X.'


def cost():
    d = Drawing('cost', 380, 'Earlier accounting: every upload inspected the existing inventory')
    d.text(30, 35, 'SAME NEW OUTPUT: 1 KiB', 15, BLUE)
    for y, label, count in [(90, 'Small store', 4), (223, 'Larger store', 20)]:
        d.text(30, y, label, 17)
        for i in range(count):
            x = 205+(i % 10)*44
            yy = y-28+(i//10)*48
            d.rect(x, yy, 32, 36, '#e3f1eb', GREEN, 2)
            d.line(x+7, yy+12, x+25, yy+12, GREEN)
            d.line(x+7, yy+21, x+25, yy+21, GREEN)
        below = y + (45 if count == 4 else 82)
        d.line(197, below, 653, below, BLUE, arrow=True)
        d.text(421, below+22, 'inspect the existing inventory', 13, BLUE, 'middle')
    d.text(30, 367, 'Drawn file counts illustrate growth; these are not benchmark results.', 14)
    return d.finish(), [], 'The earlier accounting algorithm inspected existing files and database records on each upload. Receipt now maintains running totals; maintenance still inspects the inventory. See the selected measurements for the comparison.'


def collection():
    d = Drawing('collection', 450, 'Collection keeps files referenced by surviving records')
    d.text(25, 32, '1. SURVIVING RECORDS', 13, BLUE)
    d.text(285, 32, '2. NEEDED HASHES', 13, GREEN)
    d.text(533, 32, '3. FILE INVENTORY', 13, INK)
    for y, label in [(95, 'K1: X, Y'), (235, 'K2: X, Y')]:
        d.rect(25, y, 155, 62, '#ffffff', BLUE)
        d.text(102, y+38, label, 18, BLUE, 'middle')
        d.line(180, y+22, 309, 123, GREEN, arrow=True)
        d.line(180, y+45, 309, 259, GREEN, arrow=True)
    for y, label in [(123, 'X'), (259, 'Y')]:
        d.dot(338, y, label)
        d.line(359, y, 525, y, GREEN, arrow=True)
        d.file(535, y-38, label)
        d.text(615, y+6, 'KEEP', 15, GREEN)
    d.file(535, 331, 'Z', RED)
    d.text(310, 375, 'no reference →', 15, RED)
    d.text(610, 365, 'eligible', 14, RED)
    d.text(610, 387, 'to delete', 14, RED)
    d.text(25, 436, 'X appears twice on the left, once in the lookup map, and once on disk.', 15)
    return d.finish(), [], 'Trace X from both records into one map entry. Collection checks every inventoried file against that map. Z is a deletion candidate; a dry run reports it, while --apply may delete it. Quarantined records with references also keep their files.'


def validation():
    d = Drawing('validation', 530, 'Invalid uploads stop at the check they fail, before storage')
    stages = [('Body size', 'too large'), ('Envelope fields', 'unknown / duplicate field'), ('Payload checksum', 'bytes do not match digest'), ('Result fields', 'wrong type / missing field'), ('Meaning and key', 'invalid command / key mismatch')]
    d.line(170, 45, 170, 475, BLUE, arrow=True)
    for i, (label, failure) in enumerate(stages):
        y = 60+i*80
        d.step(i+1)
        d.rect(35, y, 270, 48, '#ffffff', BLUE)
        d.text(170, y+30, label, 17, INK, 'middle')
        d.line(305, y+24, 409, y+24, RED, arrow=True)
        d.text(430, y+18, 'STOP', 12, RED)
        d.text(430, y+39, failure, 14, RED)
        d.end()
    d.step(6)
    d.rect(35, 474, 270, 42, '#e3f1eb', GREEN)
    d.text(170, 501, 'Storage may now be called', 15, GREEN, 'middle')
    d.end()
    return d.finish(), ['Reject a body that exceeds the byte limit.', 'Check the envelope fields, including duplicate and unknown names.', 'Check that the payload bytes match their checksum.', 'Decode the result and its nested records using their field rules.', 'Check valid values, the manifest-derived key and the requested URL key.', 'Only a request that passes every check reaches storage.'], 'Follow the blue route for a valid upload. Each red exit shows a different invalid request—not one request that fails repeatedly. Authentication and permission checks happen before this sequence; a checksum does not establish truth.'


def permissions():
    d = Drawing('permissions', 370, 'Authentication and permission checks are separate decisions')
    d.text(30, 35, 'REQUEST: save a shared result', 19)
    for y, who, valid, scope, outcome, color in [(105, 'Unknown token', 'no', '—', '401', RED), (195, 'Alice: read only', 'yes', 'no', '403', GOLD), (285, 'CI: publish', 'yes', 'yes', 'storage', GREEN)]:
        d.text(25, y+6, who, 16)
        d.line(190, y, 671, y, '#c8d1d4')
        d.dot(290, y, valid, GREEN if valid == 'yes' else RED)
        if valid == 'yes':
            d.line(312, y, 438, y, color, arrow=True)
            d.dot(465, y, scope, color)
        d.rect(569, y-23, 115, 46, '#ffffff', color)
        d.text(626, y+6, outcome, 18, color, 'middle')
    d.text(290, 64, 'Token valid?', 14, BLUE, 'middle')
    d.text(465, 64, 'May publish?', 14, BLUE, 'middle')
    d.text(25, 351, 'A valid credential is not permission to perform every operation.', 17)
    return d.finish(), [], 'Compare Alice and CI: both tokens are valid, but only CI has publish permission. An authentication-backend failure is different again: the server returns 503, not a verdict that the token is invalid. These labels are not verified human identities.'


def revocation():
    d = Drawing('revocation', 420, 'Revoking a credential does not cancel a request already admitted')
    d.text(25, 33, 'Execution order → (not a duration chart)', 14)
    for y, label in [(105, 'Upload A'), (213, 'Operator'), (320, 'Request B')]:
        d.text(25, y, label, 16)
        d.line(140, y, 685, y, '#c8d1d4')
    d.step(1)
    d.dot(185, 105, '✓')
    d.text(185, 77, 'approved', 14, GREEN, 'middle')
    d.rect(218, 87, 190, 36, '#dfedf7', BLUE)
    d.text(310, 111, 'in progress', 14, BLUE, 'middle')
    d.end()
    d.step(2)
    d.dot(435, 213, '×', RED)
    d.line(435, 55, 435, 355, RED, dashed=True)
    d.text(435, 253, 'revocation commits', 15, RED, 'middle')
    d.end()
    d.step(3)
    d.line(409, 105, 650, 105, GREEN, arrow=True)
    d.text(550, 80, 'may still finish', 15, GREEN, 'middle')
    d.dot(578, 320, '×', RED)
    d.text(578, 353, 'new check → 401', 15, RED, 'middle')
    d.text(25, 399, 'The decision belongs to the request’s credential check, not its finish time.', 16)
    d.end()
    return d.finish(), ['Upload A passes its credential check and continues.', 'The operator revokes the credential; the database change commits.', 'A may finish. Request B checks the now-revoked credential and is refused.'], 'Watch the vertical red line. It changes later checks; it does not reach back and cancel Upload A. The timeline shows one possible execution, not a guarantee about network arrival order.'


def failed_save_states():
    d = Drawing('failed-save-states', 380, 'Two failed saves leave different files behind, but neither publishes a result')
    d.text(25, 32, 'FAILURE POINT', 13, INK)
    d.text(320, 32, 'FILES LEFT ON DISK', 13, GREEN)
    d.text(565, 32, 'NEW SQL RESULT', 13, BLUE)
    for y, label, files in [(100, 'Saving stderr fails', ['X']), (245, 'Database insert fails', ['X', 'Y'])]:
        d.text(25, y+20, label, 17, RED)
        d.line(25, y+42, 295, y+42, RED, arrow=True)
        for i, name in enumerate(files):
            d.file(330+i*85, y-15, name)
        d.rect(565, y-15, 125, 76, '#ffffff', BLUE)
        d.text(627, y+29, 'none', 20, BLUE, 'middle')
    d.text(25, 345, 'X = stdout file; Y = stderr file. These are two separate failed attempts.', 15)
    d.text(25, 370, 'An error does not mean that the filesystem returned to its starting state.', 16)
    return d.finish(), [], 'Compare what remains, not just the error returned. Assume these are new outputs with no other references. In the first example, stderr fails before publishing its file; in the second, both files exist before the database insert fails. Temporary files are omitted.'


def cleanup_retry():
    d = Drawing('cleanup-retry', 455, 'A cleanup retry examines what remains rather than undoing earlier deletions')
    d.text(25, 32, 'EXECUTION ORDER ↓', 13)
    for x, label in [(315, 'X: orphan'), (465, 'Y: orphan'), (615, 'L: referenced')]:
        d.text(x, 65, label, 15, INK, 'middle')
    rows = [(95, 'Before cleanup', ['X', 'Y', 'L']), (225, 'After deletion fails', ['Y', 'L']), (355, 'After successful retry', ['L'])]
    for index, (y, label, remaining) in enumerate(rows):
        d.step(index+1)
        d.text(25, y+40, label, 16)
        for x, name in [(286, 'X'), (436, 'Y'), (586, 'L')]:
            if name in remaining:
                d.file(x, y, name, GREEN if name == 'L' else BLUE)
            else:
                d.line(x+5, y+17, x+53, y+59, '#8b969b')
                d.line(x+53, y+17, x+5, y+59, '#8b969b')
                d.text(x+29, y+95, 'gone', 13, '#526477', 'middle')
        d.end()
    d.step(2)
    d.text(25, 200, 'delete X ✓; delete Y fails', 14, RED)
    d.end()
    d.step(3)
    d.text(25, 330, 'rescan, then delete Y ✓', 14, GREEN)
    d.end()
    return d.finish(), ['X and Y are unreferenced files. A surviving result still needs L.', 'Cleanup deletes X, then fails to delete Y. X stays deleted; Y and L remain.', 'A retry rebuilds the reference map and file inventory. If Y is still unreferenced and deletion succeeds, only L remains.'], 'Read downward. The crossed-out X never returns. The retry makes a fresh decision from surviving records and files; it does not resume an old deletion list blindly. This example assumes references do not change between attempts.'


def scan_gap():
    d = Drawing('scan-gap', 325, 'Two matching scans can miss a temporary change during execution')
    d.text(25, 33, 'One possible execution →', 16)
    for x, color, name in [(80, BLUE, 'A'), (330, RED, 'B'), (580, BLUE, 'A')]:
        d.file(x, 110, name, color)
    d.line(150, 147, 310, 147, RED, arrow=True)
    d.line(400, 147, 560, 147, BLUE, arrow=True)
    d.text(232, 126, 'file changes', 14, RED, 'middle')
    d.text(482, 126, 'file changes back', 14, BLUE, 'middle')
    d.text(109, 85, 'Receipt scans', 16, BLUE, 'middle')
    d.text(359, 85, 'pytest reads', 16, RED, 'middle')
    d.text(609, 85, 'Receipt scans', 16, BLUE, 'middle')
    d.line(109, 211, 609, 211, BLUE)
    d.text(359, 245, 'Receipt compares A with A: equal', 19, BLUE, 'middle')
    d.text(359, 289, 'But the test used B. Scanning is not freezing the files.', 17, RED, 'middle')
    return d.finish(), [], 'Look at the middle file, then the two endpoints. Both Receipt scans can agree while the command reads different bytes between them. A and B label file contents, not filenames.'


def conflict_state():
    d = Drawing('conflict-state', 375, 'A rejected conflicting upload still changes the stored quarantine state')
    d.text(25, 35, 'SAME INPUT KEY K; DIFFERENT OBSERVED OUTPUT', 14)
    d.rect(25, 85, 240, 70, '#e3f1eb', GREEN)
    d.text(145, 114, 'K: reusable', 19, GREEN, 'middle')
    d.text(145, 140, 'saved output X', 15, GREEN, 'middle')
    d.line(265, 120, 432, 120, RED, arrow=True)
    d.text(350, 86, 'upload Y ≠ X', 15, RED, 'middle')
    d.rect(445, 85, 250, 70, '#fff0ef', RED)
    d.text(570, 115, 'K: quarantined', 19, RED, 'middle')
    d.text(570, 140, 'do not reuse', 15, RED, 'middle')
    d.line(570, 155, 570, 230, RED, arrow=True)
    d.text(385, 200, '1. commit warning', 15, RED)
    d.text(570, 260, '2. return conflict error', 17, RED, 'middle')
    d.text(25, 321, 'The upload fails as a reusable result, but its warning must survive.', 17)
    d.text(25, 350, 'Quarantine does not decide which observation was truthful.', 16)
    return d.finish(), [], 'Follow the change to K before the error is returned. Rolling back the warning simply because the operation returns an error would let clients keep reusing a disputed result.'


def issuance_failure():
    d = Drawing('issuance-failure', 365, 'A committed credential can survive failure to print its secret')
    for x, label in [(125, 'Database'), (570, 'Operator terminal')]:
        d.text(x, 40, label, 18, INK, 'middle')
    d.step(1)
    d.rect(25, 85, 225, 95, '#e3f1eb', GREEN)
    d.text(137, 116, 'Credential row saved', 17, GREEN, 'middle')
    d.text(137, 143, 'ID + token hash', 16, GREEN, 'middle')
    d.text(137, 166, 'not the secret itself', 14, GREEN, 'middle')
    d.end()
    d.step(2)
    d.line(265, 130, 530, 130, RED, arrow=True)
    d.text(400, 106, 'printing fails', 16, RED, 'middle')
    d.dot(570, 130, '×', RED)
    d.text(570, 190, 'no complete secret', 16, RED, 'middle')
    d.end()
    d.step(3)
    d.text(25, 255, 'Recovery: list the public ID → revoke it → issue a replacement', 18)
    d.text(25, 299, 'The saved hash cannot be turned back into the generated secret.', 16)
    d.text(25, 337, 'Known committed row; uncertain commit acknowledgement is a different case.', 14)
    d.end()
    return d.finish(), ['Issuance commits the credential row before printing its secret.', 'Printing fails. The row remains even if the operator never receives a usable token.', 'Use the public ID to revoke that row, then issue a replacement.'], 'Watch which side succeeded. Failure to deliver the secret does not undo the committed credential. This is why issuance errors need recovery steps, not just a retry with no inspection.'


def local_measurements():
    d = Drawing('local-measurements', 330, 'A measured CodeCraft hit was faster, but still did input work', measured=True)
    d.text(25, 32, 'ONE CODECRAFT SELECTION · 15 PASSING TESTS', 14)
    for y, kind, label, color in [(100,'direct','Direct pytest',BLUE),(180,'miss','First Receipt run',GOLD),(260,'hit','Receipt hit',GREEN)]:
        samples = [s['client_ns']/1e6 for s in LOCAL_SAMPLES if s['kind']==kind]
        value = statistics.median(samples)
        d.text(25,y,label,16)
        d.rect(205,y-25,value*.66,36,'#edf3ef',color,2)
        d.text(213+value*.66,y,f'{value:.0f} ms',15,color)
    d.text(25,313,'Medians of 5 direct / 5 hit runs; the first Receipt run is one sample.',14)
    return d.finish(), [], 'Compare the bars on one duration scale. The miss pays for execution and two input scans. The hit avoids pytest but still scans inputs. Ordered local samples with warm OS caches are not a general speedup guarantee.'


def measured_wait():
    sample = UPLOAD_WAIT_SAMPLE
    n=len(sample['sorted_client_ns'])
    total=statistics.mean(sample['sorted_client_ns'])/1e6
    wait=sample['server_stage_totals_ns']['db_lock']/n/1e6
    d=Drawing('measured-wait',315,'Concurrent uploads spent much of their measured time waiting for the database lock', measured=True)
    d.text(25,32,'128 ENTRIES · 4 CLIENT WORKERS · 32 REPEATED UPLOADS',14)
    for y,label,value,color in [(110,'Whole request',total,BLUE),(215,'Database lock call',wait,GOLD)]:
        d.text(25,y,label,16)
        d.rect(230,y-28,value*5,40,'#f0f3ef',color,2)
        d.text(240+value*5,y,f'{value:.1f} ms',15,color)
    d.text(25,283,'Mean durations, same scale. The lock call is inside the request—not extra.',15)
    return d.finish(), [], 'The shorter bar measures the advisory-lock call, including its database round trip and wait. Do not add it to the whole-request bar. These were duplicate uploads on one local server; this is evidence of waiting in this experiment, not a production throughput limit.'


def cargo_graph():
    d = Drawing('cargo-graph', 405, 'One selected crate can reach the same dependency through two branches')
    d.text(25, 30, 'ILLUSTRATIVE PACKAGE GRAPH · ARROWS MEAN “NEEDS”', 14)
    for x, y, label in [(270, 60, 'Selected crate'), (80, 175, 'Library A'), (460, 175, 'Test helper B'), (270, 285, 'Common C')]:
        d.rect(x, y, 180, 48, '#edf5ef', GREEN)
        d.text(x+90, y+30, label, 18, INK, 'middle')
    d.line(300, 108, 170, 175, arrow=True)
    d.line(420, 108, 550, 175, arrow=True)
    d.text(25, 143, 'normal dependency', 14, BLUE)
    d.text(530, 143, 'test dependency', 14, BLUE)
    d.line(170, 223, 300, 285, arrow=True)
    d.line(550, 223, 420, 285, arrow=True)
    d.text(360, 362, 'First visit to C: inspect it. Second visit: already seen → skip.', 16, GREEN, 'middle')
    d.text(360, 389, 'A and B’s own test-only dependencies are not needed here.', 14, INK, 'middle')
    return d.finish(), [], 'Follow both branches down to C. A map of visited IDs prevents repeated inspection; a pending slice holds the remaining packages. This graph is illustrative, not regex-syntax’s actual dependency graph.'


def cargo_refusal():
    d = Drawing('cargo-refusal', 380, 'Unsupported compilation code branches to execution before lookup')
    d.text(25, 30, 'POLICY CHECK BEFORE ANY SAVED RESULT IS USED', 14)
    d.rect(230, 55, 260, 55)
    d.text(360, 89, 'Inspect selected packages', 18, INK, 'middle')
    d.line(290, 110, 175, 175, GREEN, arrow=True)
    d.line(430, 110, 545, 175, RED, arrow=True)
    d.text(100, 148, 'supported', 15, GREEN)
    d.text(545, 145, 'build.rs / macro', 15, RED)
    d.rect(50, 175, 250, 65, '#edf5ef', GREEN)
    d.text(175, 213, 'Hash declared inputs', 18, GREEN, 'middle')
    d.rect(420, 175, 250, 65, '#faeeee', RED)
    d.text(545, 204, 'Run Cargo uncached', 18, RED, 'middle')
    d.text(545, 226, 'No lookup. No publication.', 14, RED, 'middle')
    d.line(175, 240, 175, 282, GREEN, arrow=True)
    d.rect(50, 282, 250, 55, '#edf5ef', GREEN)
    d.text(175, 316, 'Look up this input key', 18, GREEN, 'middle')
    d.text(420, 295, 'Even a saved success', 16)
    d.text(420, 322, 'cannot override this branch.', 16)
    d.text(360, 366, 'The actual Cargo command and its exit code remain unchanged.', 15, INK, 'middle')
    return d.finish(), [], 'The red branch does not reach the saved-result lookup. A transitive dependency with a build script is enough to take it. This protects the refusal policy; it is not a sandbox for the command that runs.'


def cargo_cost():
    d = Drawing('cargo-cost', 335, 'A small warm Rust suite ran faster directly than through a Receipt hit', measured=True)
    d.text(25, 32, 'REGEX-SYNTAX · 147 TESTS · WARM BUILD CACHE', 14)
    for y, kind, label, color in [(100, 'direct', 'Direct (2 runs)', BLUE), (180, 'miss', 'Miss (1 run)', GOLD), (260, 'hit', 'Hit (2 runs)', GREEN)]:
        value = statistics.mean(s['client_ns']/1e6 for s in CARGO_SAMPLES if s['kind'] == kind)
        d.text(25, y, label, 16)
        d.rect(195, y-25, value*.43, 36, '#edf3ef', color, 2)
        d.text(205+value*.43, y, f'{value:.0f} ms', 15, color)
    d.text(25, 311, 'Means, one scale. Other verification overlapped; not a benchmark distribution.', 14)
    return d.finish(), [], 'A hit skipped the tests but still paid for tool and input checks. For these short tests, that was more work than running Cargo directly. The small, partly overlapping sample does not establish typical latency or a general slowdown.'


def cargo_mutation():
    d = Drawing('cargo-mutation', 355, 'Changing a copied dependency prevents reuse despite an unchanged lockfile')
    d.text(25, 30, 'REGRESSION SEQUENCE · KEYS A AND B ARE ILLUSTRATIVE', 14)
    d.text(25, 65, 'Cargo.lock is unchanged throughout.', 17)
    for x, title, body, key, outcome, color in [
        (25, 'Original dependency', 'Valid source', 'Key A', 'Execute → save success', GREEN),
        (265, 'Edited copy', 'Compiler error added', 'Key B', 'Execute → fail; no save', RED),
        (505, 'Restored copy', 'Original bytes again', 'Key A', 'Replay saved success', GREEN),
    ]:
        d.rect(x, 95, 190, 160, '#f7f8f4', color)
        d.text(x+95, 125, title, 16, INK, 'middle')
        d.text(x+95, 157, body, 14, INK, 'middle')
        d.text(x+95, 195, key, 20, color, 'middle')
        d.text(x+95, 230, outcome, 14, color, 'middle')
    d.line(215, 178, 265, 178, arrow=True)
    d.line(455, 178, 505, 178, arrow=True)
    d.text(25, 290, 'Before the edited run: clear only the fixture’s compiled dependency files.', 15)
    d.text(25, 319, 'Otherwise Cargo may reuse its own build output even when Receipt executes.', 15)
    return d.finish(), [], 'The middle state cannot reuse key A. The test keeps the lockfile unchanged and edits only an isolated dependency copy. Restoring the original bytes brings key A back. Clearing temporary build artifacts makes Cargo compile the deliberate error; Receipt does not automatically clean Cargo builds.'


def cargo_hardening_cost():
    d = Drawing('cargo-hardening-cost', 390, 'Sequential measurements show a benefit only for the longer-running selected suite', measured=True)
    d.text(25, 30, 'MEDIAN CLIENT TIME · FIVE DIRECT RUNS AND FIVE HITS PER SUITE', 14)
    for top, workload in zip([70, 215], CARGO_WORKLOADS):
        d.text(25, top, workload['name'], 18)
        for offset, kind, label, color in [(38, 'direct', 'Direct', BLUE), (85, 'hit', 'Hit', GREEN)]:
            value = statistics.median(s['client_ns']/1e6 for s in workload['samples'] if s['kind'] == kind)
            d.text(25, top+offset, label, 16)
            d.rect(125, top+offset-23, value*.16, 30, '#edf3ef', color, 2)
            d.text(135+value*.16, top+offset-2, f'{value:,.0f} ms', 15, color)
    d.text(25, 345, 'One scale. Warm builds; sequential runs; no overlapping agent test work.', 14)
    d.text(25, 373, 'Not randomized or machine-isolated. No remote-cache measurement.', 14)
    return d.finish(), [], 'Regex-syntax was quicker directly: 139 ms versus a 434 ms hit. Unicode-width took 3,049 ms directly versus a 392 ms hit. Each hit still checks inputs, so the benefit depends on how much execution it skips. Source: the linked raw sequential measurement record.'


FIGURES = {
    'figure-7-cargo-mutation': cargo_mutation, 'figure-7-cargo-hardening-cost': cargo_hardening_cost,
    'figure-7-cargo-graph': cargo_graph, 'figure-7-cargo-refusal': cargo_refusal, 'figure-7-cargo-cost': cargo_cost,
    'figure-6-local-measurements': local_measurements, 'figure-6-measured-wait': measured_wait,
    'figure-1-scan-gap': scan_gap, 'figure-2-conflict-state': conflict_state,
    'figure-4-failed-save-states': failed_save_states, 'figure-4-cleanup-retry': cleanup_retry,
    'figure-5-issuance-failure': issuance_failure,
    'figure-1-manifest': manifest, 'figure-1-key-change': key_change,
    'figure-1-local-flow': local_flow, 'figure-2-admission': admission,
    'figure-2-storage': storage, 'figure-2-interrupted-write': interrupted,
    'figure-3-reader-cleanup': locks, 'figure-3-upload-cost': cost,
    'figure-3-collection-map': collection, 'figure-4-validation': validation,
    'figure-5-permissions': permissions, 'figure-5-revocation': revocation,
}



def outputs():
    """Return every file this script owns, mapped to its expected content."""
    markdown_path = ROOT / 'FIGURES.md'
    markdown = markdown_path.read_text()
    files = {}
    for name, draw in FIGURES.items():
        svg, steps, watch = draw()
        files[ROOT / 'assets' / 'reviews' / (name + '.svg')] = svg + '\n'
        pattern = rf'(<!-- review-figure:{name}:start -->\n)(.*?)(\n<!-- review-figure:{name}:end -->)'
        match = re.search(pattern, markdown, re.S)
        if not match:
            raise ValueError('Missing Markdown figure: ' + name)
        body = re.sub(r'<!-- visual-summary:start -->.*?<!-- visual-summary:end -->\n*', '', match[2], flags=re.S)
        heading = re.search(r'#### (.*?)\n', body)
        summary = f'\n<!-- visual-summary:start -->\n![{heading[1]}](assets/reviews/{name}.svg)\n\n{watch}\n'
        if steps:
            summary += '\n' + '\n'.join(f'{i+1}. {s}' for i, s in enumerate(steps)) + '\n'
        summary += '<!-- visual-summary:end -->\n'
        body = body[:heading.end()] + summary + body[heading.end():]
        markdown = markdown[:match.start(2)] + body + markdown[match.end(2):]
    files[markdown_path] = markdown
    return files


def stale():
    return [str(p.relative_to(ROOT)) for p, content in outputs().items()
            if not p.exists() or p.read_text() != content]


def main():
    if '--check' in sys.argv:
        names = stale()
        if names:
            raise SystemExit('Stale review illustrations: ' + ', '.join(names))
        print('Review illustrations are current.')
        return
    for p, content in outputs().items():
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    print(f'Rendered {len(FIGURES)} illustrations and their Markdown summaries.')


if __name__ == '__main__':
    main()
