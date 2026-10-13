"""Generate the dated deployment appendix from PHASE-REVIEWS.md, not a second prose copy."""
import re
from render_working_doc import render

HEADING = '## Deployment review: one server over HTTPS — 13 October 2026'
START = '<!-- deployment-review:start -->'
END = '<!-- deployment-review:end -->'


def update_deployment(document, markdown):
    if HEADING not in markdown:
        return document
    section = markdown.split(HEADING, 1)[1].split('\n## ', 1)[0]
    page = render('# Deployment review\n\n' + HEADING + section)
    article = re.search(r'<article .*?</article>', page, re.S)[0]
    article = re.sub(r'<div class="adjacent">.*?</div>', '', article, flags=re.S)
    article = article.replace(' data-chapter', '')
    article = re.sub(r'<div class="chapter-number"[^>]*>.*?</div>', '', article)
    block = START + '\n' + article + '\n' + END
    if START in document:
        document = re.sub(re.escape(START) + '.*?' + re.escape(END), lambda _: block,
                          document, flags=re.S)
    else:
        document = document.replace('</main>', block + '\n</main>', 1)
    link = '<li><a href="#deployment-review-one-server-over-https-13-october-2026">Deployment: one server over HTTPS</a></li>'
    if link not in document:
        document = document.replace('</ol></details></nav>', link + '</ol></details></nav>', 1)
    return document
