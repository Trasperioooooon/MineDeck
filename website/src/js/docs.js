/* 文档中心客户端交互：懒加载静态搜索索引（HTTP fetch + file:// preview script 兼容）、中英文分词排序、ARIA combobox、快捷键与窄屏目录折叠。 */
(function () {
  var boxes = [].slice.call(document.querySelectorAll('[data-doc-search]'));
  if (!boxes.length) return;

  var cache = {};
  var CJK_RUN = /[\u4e00-\u9fff]+/g;
  var LATIN_TOK = /[a-z0-9][a-z0-9._-]*/g;

  function esc(s) {
    return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function escRe(s) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function tokenizeText(str) {
    var low = String(str || '').toLowerCase();
    var bigrams = [];
    var unigrams = [];
    var words = [];
    var m;
    CJK_RUN.lastIndex = 0;
    while ((m = CJK_RUN.exec(low))) {
      var run = m[0];
      for (var i = 0; i < run.length; i++) {
        unigrams.push(run[i]);
        if (i + 1 < run.length) bigrams.push(run.slice(i, i + 2));
      }
    }
    LATIN_TOK.lastIndex = 0;
    while ((m = LATIN_TOK.exec(low))) {
      words.push(m[0]);
      if (m[0].indexOf('-') >= 0) {
        m[0].split('-').forEach(function (part) { if (part) words.push(part); });
      }
    }
    return { low: low, bigrams: bigrams, unigrams: unigrams, words: words };
  }

  function buildEngine(raw) {
    var docs = (raw && raw.docs) || [];
    var biMap = Object.create(null);
    var uniMap = Object.create(null);

    function addPost(map, key, docIdx, secIdx, mask) {
      if (!key) return;
      var row = map[key];
      if (!row) row = map[key] = Object.create(null);
      var id = docIdx + ':' + secIdx;
      row[id] = (row[id] || 0) | mask;
    }

    var prepared = docs.map(function (d, di) {
      var tkT = tokenizeText(d.t);
      var tkD = tokenizeText(d.d);
      var tkK = tokenizeText((d.k || []).join(' '));
      var secs = (d.p || []).map(function (s, si) {
        var tkH = tokenizeText(s.h);
        var tkX = tokenizeText(s.x);
        tkT.bigrams.forEach(function (b) { addPost(biMap, b, di, si, 8); });
        tkK.bigrams.forEach(function (b) { addPost(biMap, b, di, si, 4); });
        tkH.bigrams.forEach(function (b) { addPost(biMap, b, di, si, 2); });
        tkX.bigrams.forEach(function (b) { addPost(biMap, b, di, si, 1); });
        tkD.bigrams.forEach(function (b) { addPost(biMap, b, di, si, 1); });
        tkT.unigrams.forEach(function (u) { addPost(uniMap, u, di, si, 8); });
        tkK.unigrams.forEach(function (u) { addPost(uniMap, u, di, si, 4); });
        tkH.unigrams.forEach(function (u) { addPost(uniMap, u, di, si, 2); });
        tkX.unigrams.forEach(function (u) { addPost(uniMap, u, di, si, 1); });
        return {
          a: s.a || '',
          h: s.h || '',
          x: s.x || '',
          lowH: tkH.low,
          lowX: tkX.low,
          wordsH: tkH.words,
          wordsX: tkX.words,
        };
      });
      return {
        raw: d,
        lowT: tkT.low,
        lowD: tkD.low,
        lowK: tkK.low,
        wordsT: tkT.words,
        wordsD: tkD.words,
        wordsK: tkK.words,
        secs: secs,
      };
    });

    function search(query) {
      var q = tokenizeText(query.trim());
      if (!q.low) return [];
      var qBi = Array.from(new Set(q.bigrams));
      var qUni = Array.from(new Set(q.unigrams));
      var qWords = Array.from(new Set(q.words));
      var secScores = Object.create(null);
      var hasMulti = qBi.length > 0 || qWords.length > 0;

      qBi.forEach(function (b) {
        var row = biMap[b];
        if (!row) return;
        for (var id in row) {
          var m = row[id];
          var add = 0;
          if (m & 8) add += 14;
          if (m & 4) add += 11;
          if (m & 2) add += 9;
          if (m & 1) add += 3;
          secScores[id] = (secScores[id] || 0) + add;
        }
      });

      if (!qBi.length || qUni.length) {
        qUni.forEach(function (u) {
          var row = uniMap[u];
          if (!row) return;
          var scale = hasMulti ? 0.15 : 1;
          for (var id in row) {
            var m = row[id];
            var add = 0;
            if (m & 8) add += 8 * scale;
            if (m & 4) add += 6 * scale;
            if (m & 2) add += 5 * scale;
            if (m & 1) add += 1.5 * scale;
            secScores[id] = (secScores[id] || 0) + add;
          }
        });
      }

      if (qWords.length) {
        prepared.forEach(function (d, di) {
          var docBase = 0;
          qWords.forEach(function (w) {
            if (d.wordsT.some(function (x) { return x === w; })) docBase += 14;
            else if (d.wordsT.some(function (x) { return x.indexOf(w) === 0; })) docBase += 10;
            else if (d.lowT.indexOf(w) >= 0) docBase += 7;

            if (d.wordsK.some(function (x) { return x === w; })) docBase += 11;
            else if (d.wordsK.some(function (x) { return x.indexOf(w) === 0; })) docBase += 8;

            if (d.wordsD.some(function (x) { return x.indexOf(w) === 0; })) docBase += 4;
          });
          d.secs.forEach(function (s, si) {
            var sScore = docBase;
            qWords.forEach(function (w) {
              if (s.wordsH.some(function (x) { return x === w; })) sScore += 9;
              else if (s.wordsH.some(function (x) { return x.indexOf(w) === 0; })) sScore += 6.5;
              else if (s.lowH.indexOf(w) >= 0) sScore += 4.5;

              if (s.wordsX.some(function (x) { return x === w; })) sScore += 3.5;
              else if (s.wordsX.some(function (x) { return x.indexOf(w) === 0; })) sScore += 2.2;
              else if (s.lowX.indexOf(w) >= 0) sScore += 1.4;
            });
            if (sScore > 0) {
              var id = di + ':' + si;
              secScores[id] = (secScores[id] || 0) + sScore;
            }
          });
        });
      }

      var bestByDoc = Object.create(null);
      for (var id in secScores) {
        var parts = id.split(':');
        var di = +parts[0], si = +parts[1];
        var d = prepared[di], s = d.secs[si];
        var sc = secScores[id];
        if (q.low.length >= 2) {
          if (d.lowT.indexOf(q.low) >= 0) sc += 45;
          if (d.lowK.indexOf(q.low) >= 0) sc += 32;
          if (s.lowH.indexOf(q.low) >= 0) sc += 26;
          if (d.lowD.indexOf(q.low) >= 0) sc += 14;
          if (s.lowX.indexOf(q.low) >= 0) sc += 12;
        }
        var cur = bestByDoc[di];
        if (!cur || sc > cur.score) bestByDoc[di] = { di: di, si: si, score: sc };
      }

      var list = Object.keys(bestByDoc).map(function (k) { return bestByDoc[k]; });
      list.sort(function (a, b) { return b.score - a.score; });
      return list.slice(0, 10).map(function (hit) {
        var d = prepared[hit.di].raw;
        var sec = (d.p && d.p[hit.si]) || { a: '', h: '', x: d.d };
        return {
          doc: d,
          anchor: sec.a,
          heading: sec.h,
          snippetSrc: sec.x || d.d,
          terms: qBi.concat(qWords).concat(!qBi.length ? qUni : []).slice(0, 8),
        };
      });
    }

    return { search: search };
  }

  function loadIndex(box) {
    var lang = box.getAttribute('data-lang') || 'zh';
    if (cache[lang]) return cache[lang];
    var isFile = location.protocol === 'file:';
    var jsonUrl = box.getAttribute('data-index-json');
    var jsUrl = box.getAttribute('data-index-js');

    function loadViaScript() {
      return new Promise(function (resolve, reject) {
        if (window.__MD_DOCS_IDX && window.__MD_DOCS_IDX[lang]) {
          resolve(buildEngine(window.__MD_DOCS_IDX[lang]));
          return;
        }
        var s = document.createElement('script');
        s.src = jsUrl;
        s.onload = function () {
          if (window.__MD_DOCS_IDX && window.__MD_DOCS_IDX[lang]) resolve(buildEngine(window.__MD_DOCS_IDX[lang]));
          else reject(new Error('index missing'));
        };
        s.onerror = function () { reject(new Error('script load failed')); };
        document.head.appendChild(s);
      });
    }

    cache[lang] = isFile || !window.fetch
      ? loadViaScript()
      : fetch(jsonUrl).then(function (r) {
          if (!r.ok) throw new Error(String(r.status));
          return r.json();
        }).then(buildEngine).catch(function () {
          return loadViaScript();
        });
    return cache[lang];
  }

  function highlightHtml(text, query, terms) {
    var s = String(text || '');
    var keys = [query.trim()].concat(terms || []).filter(function (k) { return k && k.length >= 1; });
    keys.sort(function (a, b) { return b.length - a.length; });
    var unique = [];
    keys.forEach(function (k) {
      var low = k.toLowerCase();
      if (!unique.some(function (u) { return u.toLowerCase() === low; })) unique.push(k);
    });
    if (!unique.length) return esc(s);
    var re = new RegExp('(' + unique.map(escRe).join('|') + ')', 'gi');
    var first = s.search(re);
    if (s.length > 120) {
      var start = first > 24 ? first - 24 : 0;
      s = (start > 0 ? '…' : '') + s.slice(start, start + 115) + (start + 115 < s.length ? '…' : '');
    }
    var out = '', last = 0, m;
    re.lastIndex = 0;
    while ((m = re.exec(s))) {
      if (m.index > last) out += esc(s.slice(last, m.index));
      out += '<mark>' + esc(m[0]) + '</mark>';
      last = m.index + m[0].length;
    }
    if (last < s.length) out += esc(s.slice(last));
    return out;
  }

  function wireBox(box) {
    var input = box.querySelector('.doc-search-in');
    var pop = box.querySelector('.doc-search-pop');
    var clearBtn = box.querySelector('.doc-search-clear');
    var status = box.querySelector('[data-search-status]');
    if (!input || !pop) return;

    var docsRoot = box.getAttribute('data-docs-root') || './';
    var isPreview = box.getAttribute('data-preview') === '1';
    var activeIdx = -1;
    var optionIds = [];

    function setExpanded(open) {
      pop.hidden = !open;
      input.setAttribute('aria-expanded', open ? 'true' : 'false');
      if (!open) {
        activeIdx = -1;
        input.setAttribute('aria-activedescendant', '');
      }
    }

    function setActive(idx) {
      var opts = [].slice.call(pop.querySelectorAll('[role="option"]'));
      if (!opts.length) {
        activeIdx = -1;
        input.setAttribute('aria-activedescendant', '');
        return;
      }
      if (idx < 0) idx = opts.length - 1;
      if (idx >= opts.length) idx = 0;
      activeIdx = idx;
      opts.forEach(function (el, i) { el.setAttribute('aria-selected', i === activeIdx ? 'true' : 'false'); });
      var activeEl = opts[activeIdx];
      input.setAttribute('aria-activedescendant', activeEl.id);
      if (activeEl.scrollIntoView) activeEl.scrollIntoView({ block: 'nearest' });
    }

    function renderLoading() {
      pop.innerHTML = '<div class="doc-search-msg">' + esc(box.getAttribute('data-loading')) + '</div>';
      setExpanded(true);
    }

    function runQuery() {
      var val = input.value.trim();
      if (clearBtn) clearBtn.hidden = !input.value;
      if (!val) {
        pop.innerHTML = '';
        setExpanded(false);
        if (status) status.textContent = '';
        return;
      }
      renderLoading();
      loadIndex(box).then(function (engine) {
        if (input.value.trim() !== val) return;
        var hits = engine.search(val);
        optionIds = [];
        if (!hits.length) {
          pop.innerHTML = '<div class="doc-search-msg">' + esc(box.getAttribute('data-empty')) + '</div>';
          setExpanded(true);
          if (status) status.textContent = box.getAttribute('data-empty');
          return;
        }
        var prefix = input.id + '-opt-';
        pop.innerHTML = hits.map(function (hit, idx) {
          var oid = prefix + idx;
          optionIds.push(oid);
          var url = docsRoot + hit.doc.u + (isPreview ? 'index.html' : '') + (hit.anchor ? '#' + encodeURIComponent(hit.anchor) : '');
          var subHead = hit.heading ? ' › ' + highlightHtml(hit.heading, val, hit.terms) : '';
          return '<a class="doc-search-hit" role="option" id="' + esc(oid) + '" aria-selected="' + (idx === 0 ? 'true' : 'false') + '" href="' + esc(url) + '">' +
            '<span class="doc-hit-top"><span class="doc-hit-cat">' + esc(hit.doc.c) + '</span><span class="doc-hit-title">' + highlightHtml(hit.doc.t, val, hit.terms) + subHead + '</span></span>' +
            '<span class="doc-hit-snip">' + highlightHtml(hit.snippetSrc, val, hit.terms) + '</span></a>';
        }).join('');
        setExpanded(true);
        setActive(0);
        if (status) status.textContent = box.getAttribute('data-count').replace('{n}', String(hits.length));
      }).catch(function () {
        pop.innerHTML = '<div class="doc-search-msg">' + esc(box.getAttribute('data-error')) + '</div>';
        setExpanded(true);
      });
    }

    input.addEventListener('focus', function () {
      loadIndex(box);
      if (input.value.trim()) runQuery();
    });
    input.addEventListener('input', runQuery);
    if (clearBtn) {
      clearBtn.addEventListener('click', function () {
        input.value = '';
        runQuery();
        input.focus();
      });
    }
    input.addEventListener('keydown', function (e) {
      var opts = [].slice.call(pop.querySelectorAll('[role="option"]'));
      if (e.key === 'ArrowDown' && opts.length) { e.preventDefault(); setActive(activeIdx + 1); }
      else if (e.key === 'ArrowUp' && opts.length) { e.preventDefault(); setActive(activeIdx - 1); }
      else if (e.key === 'Enter' && !pop.hidden && opts.length) {
        var target = opts[activeIdx >= 0 ? activeIdx : 0];
        if (target) { e.preventDefault(); location.href = target.getAttribute('href'); }
      } else if (e.key === 'Escape') {
        if (!pop.hidden) { e.preventDefault(); setExpanded(false); }
        else input.blur();
      }
    });
    document.addEventListener('click', function (e) {
      if (!box.contains(e.target)) setExpanded(false);
    });
  }

  boxes.forEach(wireBox);

  function focusSearch() {
    var targetBox = boxes[0];
    var treeDetails = targetBox.closest('details.doc-tree-box');
    if (treeDetails && !treeDetails.open) treeDetails.open = true;
    var input = targetBox.querySelector('.doc-search-in');
    if (input) {
      input.focus();
      input.select();
    }
  }

  document.addEventListener('keydown', function (e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      focusSearch();
      return;
    }
    if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      var tag = document.activeElement && document.activeElement.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (document.activeElement && document.activeElement.isContentEditable)) return;
      e.preventDefault();
      focusSearch();
    }
  });

  // 支持 ?q= 关键词参数直接进入搜索
  try {
    var q = new URLSearchParams(location.search).get('q');
    if (q) {
      var primary = boxes[0];
      var treeDetails = primary.closest('details.doc-tree-box');
      if (treeDetails && !treeDetails.open) treeDetails.open = true;
      var inp = primary.querySelector('.doc-search-in');
      if (inp) {
        inp.value = q;
        inp.dispatchEvent(new Event('input', { bubbles: true }));
      }
    }
  } catch (e) {}

  // 窄屏（<=900px）自动折叠左侧目录抽屉，放宽到桌面时恢复展开
  var treeBox = document.querySelector('details.doc-tree-box');
  if (treeBox && window.matchMedia) {
    var mq = window.matchMedia('(max-width: 900px)');
    var applyTree = function (first) {
      if (mq.matches) {
        if (first && !new URLSearchParams(location.search).get('q')) treeBox.open = false;
      } else {
        treeBox.open = true;
      }
      treeBox.setAttribute('data-tree-init', '1');
    };
    applyTree(true);
    if (mq.addEventListener) mq.addEventListener('change', function () { applyTree(false); });
  }
})();
