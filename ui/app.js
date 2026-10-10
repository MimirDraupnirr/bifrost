'use strict';
const $=id=>document.getElementById(id);
const api=async(m,u,b)=>{const r=await fetch(u,{method:m,headers:{'Content-Type':'application/json'},body:b?JSON.stringify(b):undefined});const j=await r.json().catch(()=>({}));if(!r.ok)throw new Error(j.error||r.statusText);return j;};
const human=n=>{const u=['o','Ko','Mo','Go','To'];let i=0;while(n>=1024&&i<4){n/=1024;i++}return n.toFixed(i?1:0).replace('.',',')+' '+u[i]};
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const ico=n=>`<svg class="i" aria-hidden="true"><use href="#i-${n}"/></svg>`;
const msg=(kind,text,extra='')=>`<div class="msg ${kind}">${ico(kind==='ok'?'check':kind==='err'?'x':kind==='warn'?'alert':'info')}<div>${text}${extra}</div></div>`;
const st={step:1,max:1,mode:'dir',hash:null,clientList:[],sort:{k:'name',d:1},job:null,sel:null,picked:null,analysis:null,templates:[],images:[],captures:[],me:null,miErr:'',state:null,music:false,mbPicked:null,mbResults:[],typesTouched:false};

// ---- retours à l'écran : toasts et boutons occupés ----
function toast(text,kind='info'){const t=document.createElement('div');t.className='toast '+kind;t.innerHTML=ico(kind==='ok'?'check':kind==='err'?'x':'info')+'<span>'+esc(text)+'</span>';$('toasts').append(t);setTimeout(()=>t.remove(),kind==='err'?8000:4000)}
// run : désactive le bouton le temps de la requête ; l'erreur part en toast.
async function run(btn,fn){if(btn.disabled)return;btn.disabled=true;btn.classList.add('busy');try{return await fn()}catch(e){toast(e.message,'err')}finally{btn.disabled=false;btn.classList.remove('busy')}}
const onSubmit=(form,fn)=>form.addEventListener('submit',e=>{e.preventDefault();fn()});
// Types d'œuvre TMDB : tmdbKind dans history.go.
const TK=Object.freeze({movie:'movie',tv:'tv'});

// ---- le rail : 4 étapes, on peut revenir sur une étape déjà atteinte ----
function go(n){st.step=n;st.max=Math.max(st.max,n);
  [...$('rail').children].forEach((li,i)=>{const k=i+1;li.className=k<n?'done reach':k===n?'on reach':k<=st.max?'reach':'';li.firstElementChild.disabled=k>st.max});
  $('railFill').style.width=((n-1)/3*100)+'%';
  [1,2,3,4].forEach(k=>$('p'+k).hidden=k!==n);scrollTop()}
// Remonter en haut APRÈS le rendu du panneau : un scrollTo lancé avant que la page ait changé de hauteur était parfois ignoré.
function scrollTop(){requestAnimationFrame(()=>{window.scrollTo({top:0,behavior:'instant'});document.documentElement.scrollTop=0;document.body.scrollTop=0})}
// Amener un élément sous l'en-tête fixe, en douceur.
function reveal(el,block='start'){if(!el)return;requestAnimationFrame(()=>{const y=el.getBoundingClientRect().top+window.scrollY-72;window.scrollTo({top:block==='start'?y:Math.max(0,y-window.innerHeight/3),behavior:'smooth'})})}
$('rail').addEventListener('click',e=>{const b=e.target.closest('button');if(b&&!b.disabled)go(+b.dataset.n)});

// ---- réglages (tiroir) ----
const drawer=$('settings');
$('gear').onclick=()=>drawer.showModal();
$('closeSettings').onclick=()=>drawer.close();
drawer.addEventListener('click',e=>{if(e.target===drawer)drawer.close()});

async function load(){const s=await api('GET','/ui/state');st.state=s;const c=s.config;
  $('version').textContent=s.version;$('site').value=c.site_url||'';$('site2').value=c.site_url||'';$('outdir').value=c.out_dir||'';
  $('mi').className='status '+(s.mediainfo?'ok':'warn');
  $('mi').innerHTML=s.mediainfo?ico('check')+'<span>mediainfo présent : les facettes seront lues dans les fichiers.</span>':ico('alert')+'<span>mediainfo absent — installe-le : <code>'+esc(s.mediainfo_hint)+'</code>. Sans lui, les facettes sont déclarées à la main et Ratatosk les vérifie.</span>';
  document.querySelector(`input[name=src][value=${c.source==='ssh'?'ssh':'local'}]`).checked=true;$('sshbox').hidden=c.source!=='ssh';
  $('cl_type').value=c.client?.type||'';$('cl_url').value=c.client?.url||'';$('cl_user').value=c.client?.user||'';$('cl_label').value=c.client?.label||'';$('cl_skip').checked=!!c.client?.skip_check;$('cl_paths').value=(c.client?.path_map||[]).map(m=>m.from+' = '+m.to).join('\n');
  $('autoUpd').checked=!!s.auto_update;$('batchIntro').open=!c.batch_intro_hidden;
  if(s.update){$('upd').hidden=false;$('updTitle').textContent='Bifröst '+s.update.version+' est disponible (tu as '+s.version+')';
    let notes=s.update.notes||'';
    if(s.in_docker)notes+='\nEn Docker : docker pull ghcr.io/mimirdraupnirr/bifrost && docker compose up -d';
    else if(s.auto_update)notes+='\nElle s\'installera toute seule au prochain lancement.';
    else $('updBtn').hidden=false;
    $('updNotes').textContent=notes.trim()}
  if(s.locked){$('lock').hidden=false;$('gear').hidden=true;
    $('lockTitle').textContent=s.needs_password?'Définis un mot de passe local':'Mot de passe local';
    $('lockMsg').textContent=s.needs_password?'Cette page n\'écoute pas sur 127.0.0.1 : sans mot de passe, n\'importe qui sur le réseau publierait avec ton jeton. 8 caractères minimum.':'Cette page est protégée.';
    $('lockBtn').textContent=s.needs_password?'Définir':'Entrer';
    onSubmit($('lockForm'),()=>run($('lockBtn'),async()=>{try{await api('POST',s.needs_password?'/ui/password':'/ui/login',{password:$('lockPass').value});location.reload()}catch(e){$('lockErr').textContent=e.message}}));
    $('lockPass').focus();return}
  $('ssh_host').value=c.ssh?.host||'';$('ssh_port').value=c.ssh?.port||22;$('ssh_user').value=c.ssh?.user||'';$('ssh_key').value=c.ssh?.key_path||'';$('ssh_dir').value=c.ssh?.data_dir||'';
  if(!s.has_token){$('connect').hidden=false;$('acctStatus').textContent='Pas encore connecté.';$('token').focus();return}
  // Jeton enregistré : jamais redemandé, on arrive directement sur les fichiers.
  $('app').hidden=false;go(1);$('who').hidden=false;$('me').textContent='connexion…';
  if(s.connected)await connected(s.me);else{try{await connect(true)}catch(e){offline(e.message)}}}

function offline(m){$('offline').hidden=false;$('offlineMsg').textContent=m+' — le jeton enregistré n\'a pas été redemandé.';$('me').textContent='hors ligne';$('acctStatus').className='status err';$('acctStatus').innerHTML=ico('cloud-off')+'<span>'+esc(m)+'</span>'}
$('retry').onclick=()=>run($('retry'),async()=>{try{await connect(true);$('offline').hidden=true}catch(e){offline(e.message)}});
$('relog').onclick=()=>{drawer.showModal();$('token2').focus()};
async function connected(me){st.me=me;$('app').hidden=false;$('connect').hidden=true;$('who').hidden=false;
  $('me').textContent=me.name+' · '+me.class+' · ratio '+(me.ratio??'∞');
  $('acctStatus').className='status ok';$('acctStatus').innerHTML=ico('check')+'<span>Connecté : <b>'+esc(me.name)+'</b> · '+esc(me.class)+' · ratio '+esc(me.ratio??'∞')+'</span>';
  $('offline').hidden=true;await cats();if(st.mode==='client'&&st.clientList.length)renderClient();else browse('').catch(e=>listEmpty('alert',e.message))}
async function connect(silent,site,token){const r=await api('POST','/ui/connect',{site_url:site??$('site').value,token:silent?'':token});$('token').value='';$('token2').value='';await connected(r.me)}
onSubmit($('connectForm'),()=>run($('connectBtn'),async()=>{$('connectErr').textContent='';try{await connect(false,$('site').value,$('token').value);$('site2').value=$('site').value;toast('Connecté','ok')}catch(e){$('connectErr').textContent=e.message}}));
onSubmit($('acctForm'),()=>run($('connect2'),async()=>{$('acctErr').textContent='';try{await connect(false,$('site2').value,$('token2').value);$('site').value=$('site2').value;toast('Connecté avec le nouveau jeton','ok');drawer.close()}catch(e){$('acctErr').textContent=e.message}}));

onSubmit($('outForm'),()=>run($('saveOut'),async()=>{await api('POST','/ui/settings',{out_dir:$('outdir').value});toast('Dossier enregistré','ok')}));
// Explication du Lot repliée ou non : gardée dans les paramètres ; repliée, le tableau prend la place.
// Repliée, le tableau gagne exactement la hauteur du texte : le panneau garde sa taille.
// Mesurée une fois par largeur (le texte se replie selon elle), sans animer la flèche pendant la mesure.
function batchIntroFit(){const d=$('batchIntro'),w=d.offsetWidth;if(!w||w===st.introW)return;const was=d.open;d.classList.add('measure');
  d.open=true;const a=d.offsetHeight;d.open=false;const h=a-d.offsetHeight;d.open=was;d.offsetHeight;d.classList.remove('measure');
  if(h>0){st.introW=w;$('batchList').style.setProperty('--intro',h+'px')}}
$('batchIntro').addEventListener('toggle',()=>{const h=!$('batchIntro').open;if(st.state&&!!st.state.config.batch_intro_hidden===h)return;if(st.state)st.state.config.batch_intro_hidden=h;api('POST','/ui/settings',{batch_intro_hidden:h}).catch(e=>toast(e.message,'err'))});
$('autoUpd').onchange=()=>api('POST','/ui/settings',{auto_update:$('autoUpd').checked}).then(()=>toast($('autoUpd').checked?'Mises à jour automatiques activées':'Mises à jour automatiques désactivées','ok'),e=>toast(e.message,'err'));
$('updBtn').onclick=()=>run($('updBtn'),async()=>{const r=await api('POST','/ui/update');$('updTitle').textContent=r.status;$('updBtn').hidden=true;setTimeout(()=>location.reload(),4000)});

document.querySelectorAll('input[name=src]').forEach(r=>r.onchange=async()=>{$('sshbox').hidden=r.value!=='ssh';try{await api('POST','/ui/settings',{source:r.value});toast(r.value==='ssh'?'Source : la seedbox':'Source : ce poste','ok');if(st.me&&st.mode==='dir')browse('')}catch(e){toast(e.message,'err')}});
const sshCfg=()=>({host:$('ssh_host').value.trim(),port:parseInt($('ssh_port').value)||22,user:$('ssh_user').value.trim(),key_path:$('ssh_key').value.trim(),data_dir:$('ssh_dir').value.trim()});
function sshSteps(list){$('sshSteps').innerHTML=list.map(x=>`<div class="${x.ok?'ok':'err'}">${ico(x.ok?'check':'x')}<span>${esc(x.label)}${x.detail?' — '+esc(x.detail):''}</span></div>`).join('')}
$('sshSave').onclick=()=>run($('sshSave'),async()=>{await api('POST','/ui/settings',{source:'ssh',ssh:sshCfg(),password:$('ssh_pass').value});sshSteps([{ok:true,label:'Réglages enregistrés'}])});
$('sshTest').onclick=()=>run($('sshTest'),async()=>{sshSteps([{ok:true,label:'Connexion en cours…'}]);$('hostkey').hidden=true;
  try{const r=await api('POST','/ui/ssh/test',{ssh:sshCfg(),password:$('ssh_pass').value});sshSteps(r.steps);
    if(r.hostkey){$('hostkeyMsg').textContent=(r.hostkey.Changed?'La clé de cet hôte a CHANGÉ. ':'Première connexion à '+r.hostkey.Host+'. ')+'Empreinte : '+r.hostkey.Fingerprint;$('hostkey').hidden=false;
      $('hostkeyAccept').onclick=()=>run($('hostkeyAccept'),async()=>{await api('POST','/ui/ssh/accept',{key:r.hostkey.Key});$('hostkey').hidden=true;$('sshTest').click()})}
    else if(r.steps.every(x=>x.ok)){document.querySelector('input[name=src][value=ssh]').checked=true;await api('POST','/ui/settings',{source:'ssh'});if(st.me&&st.mode==='dir')browse('')}}
  catch(e){sshSteps([{ok:false,label:e.message}])}});
const clCfg=()=>({type:$('cl_type').value,url:$('cl_url').value.trim(),user:$('cl_user').value.trim(),password:$('cl_pass').value,label:$('cl_label').value.trim(),skip_check:$('cl_skip').checked,
  path_map:$('cl_paths').value.split('\n').map(l=>{const i=l.indexOf('=');return i<1?{}:{from:l.slice(0,i).trim(),to:l.slice(i+1).trim()}}).filter(m=>m.from&&m.to)});
$('clDetect').onclick=()=>run($('clDetect'),async()=>{$('clMsg').className='status';$('clMsg').textContent='Sondage des ports habituels…';
  const r=await api('GET','/ui/client/detect');const c=(r.clients||[])[0];
  if(!c){$('clMsg').className='status err';$('clMsg').textContent='Aucun client trouvé sur ce poste. Active l\'interface Web du client (voir ci-dessus), ou saisis son adresse.';return}
  $('cl_type').value=c.type;$('cl_url').value=c.url;$('clMsg').className='status ok';$('clMsg').textContent=`Trouvé : ${c.type} sur ${c.url}${r.clients.length>1?' (+'+(r.clients.length-1)+' autre(s))':''}. Renseigne identifiant et mot de passe, puis Tester.`});
$('clTest').onclick=()=>run($('clTest'),async()=>{$('clMsg').className='status';$('clMsg').textContent='Connexion…';
  try{const r=await api('POST','/ui/client/test',clCfg());$('clMsg').className='status ok';$('clMsg').innerHTML=ico('check')+'<span>'+esc(r.version)+'</span>';$('cl_pass').value=''}
  catch(e){$('clMsg').className='status err';$('clMsg').innerHTML=ico('x')+'<span>'+esc(e.message)+'</span>'}});

// ---- étape 1 : fichiers ----
function listEmpty(icon,text){$('entries').innerHTML=`<div class="empty">${ico(icon)}<span>${esc(text)}</span></div>`}
function select(el){[...$('entries').querySelectorAll('.sel')].forEach(x=>x.classList.remove('sel'));el.classList.add('sel');$('prepare').disabled=false}
async function browse(p){st.mode='dir';$('modeDir').classList.add('on');$('modeClient').classList.remove('on');$('dirBar').hidden=false;$('clientBar').hidden=true;$('clientNote').hidden=true;
  st.sel=null;st.hash=null;$('prepare').disabled=true;$('entries').className='list';listEmpty('folder','Lecture du dossier…');const seq=st.browseSeq=(st.browseSeq||0)+1;
  const r=await api('GET','/ui/browse?path='+encodeURIComponent(p));if(seq!==st.browseSeq)return; /* une lecture plus récente a pris la main */$('path').value=r.path;$('path').dataset.parent=r.parent;
  $('entries').innerHTML=r.entries.map(e=>`<div class="e ${e.is_dir?'dir':''}" data-p="${esc(e.path)}" data-d="${e.is_dir?1:0}">${ico(e.is_dir?'folder':/\.(mkv|mp4|avi|m2ts|ts|iso)$/i.test(e.name)?'film':'file')}<span class="n" title="${esc(e.path)}">${esc(e.name)}</span><span class="sz">${human(e.size)}</span>${e.is_dir?`<button type="button" class="enter ghost" title="Entrer dans ce dossier">${ico('chevron-right')}</button>`:''}</div>`).join('');
  if(!r.entries.length)listEmpty('folder','Ce dossier est vide.');
  [...$('entries').querySelectorAll('.e')].forEach(d=>{d.onclick=e=>{if(e.target.closest('.enter')){browse(d.dataset.p).catch(e=>toast(e.message,'err'));return}select(d);st.sel=d.dataset.p};d.ondblclick=()=>{if(d.dataset.d==='1')browse(d.dataset.p).catch(e=>toast(e.message,'err'))}})}
$('go').onclick=()=>run($('go'),()=>browse($('path').value));
$('path').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('go').click()}});
$('up').onclick=()=>run($('up'),()=>browse($('path').dataset.parent||''));
$('modeDir').onclick=()=>{if(st.mode!=='dir')browse($('path').value).catch(e=>toast(e.message,'err'))};
$('modeClient').onclick=()=>fromClient();

try{$('hideOnSite').checked=localStorage.getItem('bf.hideOnSite')!=='0';$('onlyDone').checked=localStorage.getItem('bf.onlyDone')!=='0'}catch(e){}
// En-tête de colonne triable : so = {k, d} (d = 1 ou -1), ou null.
const sortHead=(so,key,label,cls='')=>`<button type="button" data-k="${key}" class="${so&&so.k===key?'on':''} ${so&&so.k===key&&so.d<0?'desc':''}" style="${cls}">${label}${ico('sort')}</button>`;
const stateRank=t=>t.progress<1?1:/missing|error/.test((t.state||'').toLowerCase())?3:/pause|stopped/.test((t.state||'').toLowerCase())?2:0;
function badge(t){if(t.progress<1)return `<span class="bdg part">${Math.round(t.progress*100)} %</span>`;const k=stateRank(t);return k===3?'<span class="bdg dead">fichiers absents</span>':k===2?'<span class="bdg x">en pause</span>':'<span class="bdg seed">seed</span>'}
function renderClient(){const q=$('clientQ').value.toLowerCase().trim();const hide=$('hideOnSite').checked,done=$('onlyDone').checked;try{localStorage.setItem('bf.hideOnSite',hide?'1':'0');localStorage.setItem('bf.onlyDone',done?'1':'0')}catch(e){}
  const all=st.clientList||[];const{k,d}=st.sort;
  const rows=all.filter(t=>(!hide||!t.on_draupnirr)&&(!done||t.progress>=1)&&(!q||t.name.toLowerCase().includes(q)))
    .sort((a,b)=>d*(k==='size'?a.size-b.size:k==='state'?stateRank(a)-stateRank(b)||a.name.localeCompare(b.name):a.name.localeCompare(b.name)));
  const onSite=all.filter(t=>t.on_draupnirr).length;
  $('clientNote').hidden=false;$('clientNote').className='note';$('clientNote').textContent=`${rows.length} affichée${rows.length>1?'s':''} sur ${all.length}`+(onSite?` · ${onSite} déjà sur Draupnirr`:'')+' · '+st.clientHint;
  const h=(...a)=>sortHead(st.sort,...a);
  $('entries').className='list tbl';
  $('entries').innerHTML=`<div class="e h">${h('name','Release')}${h('size','Taille','justify-content:flex-end')}${h('state','État')}<span class="eyebrow" style="line-height:34px">Draupnirr</span></div>`+
    rows.map(t=>`<div class="e ${t.on_draupnirr?'dim':''}" data-h="${esc(t.hash)}"><span class="nm" title="${esc(t.path)}"><b>${esc(t.name)}</b><small>${esc(t.path)}</small></span><span class="r">${human(t.size)}</span><span>${badge(t)}${t.copies>1?` <span class="bdg x" title="mêmes données, ${t.copies} trackers">×${t.copies}</span>`:''}</span><span>${t.on_draupnirr?`<span class="bdg site">${ico('ring')}déjà dessus</span>`:'<span class="mut dash">—</span>'}</span></div>`).join('');
  if(!rows.length)$('entries').insertAdjacentHTML('beforeend',`<div class="empty">${ico('search')}<span>${all.length?'Rien ne passe ces filtres.':'Le client ne contient aucun torrent.'}</span></div>`);
  $('entries').querySelectorAll('.h button').forEach(b=>b.onclick=()=>{st.sort=st.sort.k===b.dataset.k?{k:b.dataset.k,d:-st.sort.d}:{k:b.dataset.k,d:1};renderClient()});
  [...$('entries').querySelectorAll('[data-h]')].forEach(r=>r.onclick=()=>{select(r);const t=all.find(x=>x.hash===r.dataset.h);st.sel=t.path;st.hash=t.hash})}
['clientQ','hideOnSite','onlyDone'].forEach(id=>$(id).oninput=renderClient);
async function fromClient(){st.mode='client';$('modeClient').classList.add('on');$('modeDir').classList.remove('on');$('dirBar').hidden=true;$('clientBar').hidden=false;
  st.sel=null;st.hash=null;$('prepare').disabled=true;$('entries').className='list';listEmpty('hard-drive','Lecture du client…');$('clientNote').hidden=true;
  try{const r=await api('GET','/ui/client/list');st.clientList=r.torrents||[];
    st.clientHint=r.exportable?'le .torrent d\'origine sera exporté et re-scellé, sans re-hachage.':'ce client n\'exporte pas : les données seront re-hachées.';renderClient()}
  catch(e){listEmpty('hard-drive',e.message);$('clientNote').hidden=false;$('clientNote').className='note err';$('clientNote').textContent='Vérifie le client torrent dans les Réglages.'}}

async function cats(){const r=await api('GET','/ui/categories');const all=(r.categories||[]);const slugs=all.map(c=>c.slug);
  // Famille = le plus long autre slug dont celui-ci est l'extension (films-film → films) ; sinon racine.
  const fam=c=>slugs.filter(s=>s!==c.slug&&c.slug.startsWith(s+'-')).sort((a,b)=>b.length-a.length)[0]||c.slug;
  const groups={};all.forEach(c=>{(groups[fam(c)]||=[]).push(c)});
  $('category').innerHTML=Object.keys(groups).sort((a,b)=>a.localeCompare(b)).map(f=>{const root=all.find(c=>c.slug===f);const subs=groups[f].filter(c=>c.slug!==f).sort((a,b)=>a.name.localeCompare(b.name));
    return `<optgroup label="${esc(root?.name||f)}">${(root?[root]:[]).concat(subs).map(c=>`<option value="${esc(c.slug)}">${esc(c.name)}${c.slug===f&&subs.length?' (toute la famille)':''}</option>`).join('')}</optgroup>`}).join('');
  const film=all.find(c=>c.slug==='films-film');if(film)$('category').value=film.slug}

// ---- préparation : hachage → mediainfo → analyse ----
function stage(step,err){const order=['hachage','mediainfo','analyse'];const i=order.indexOf(step);
  [...$('prep').querySelectorAll('.stages span')].forEach((s,k)=>{s.className=err&&k===i?'err':step==='prêt'||k<i?'done':k===i?'on':''});
  $('prog').parentElement.classList.toggle('indet',step!=='hachage'&&step!=='prêt'&&!err)}
$('prepare').onclick=()=>run($('prepare'),async()=>{const r=await api('POST','/ui/prepare',{path:st.sel,category:$('category').value,hash:st.hash||''});st.hash=null;st.job=r.job;
  $('prep').hidden=false;reveal($('prep'),'center');$('prog').style.width='0';stage('hachage');$('prepStep').textContent='';$('facets').innerHTML='';$('advFacets').innerHTML='';st.picked=null;st.mbPicked=null;st.typesTouched=false;st.captures=[];st.images=[];$('description').value='';$('nfo').value='';$('preview').innerHTML='';$('imgs').innerHTML='';$('imgsNote').hidden=false;st.max=1;$('publish').disabled=false;
  await poll()});
// Préparation (hachage → mediainfo → analyse) suivie jusqu'au bout ; partagée avec la loupe du lot.
const jobLine=j=>{const[d,t]=j.progress;return j.step==='hachage'?(t?human(d)+' / '+human(t):(j.exported?'export depuis le client…':'')):j.step==='mediainfo'?'Lecture des pistes…':j.step==='analyse'?'Analyse par Draupnirr…':''};
function waitJob(id,onTick,alive=()=>true){return new Promise((res,rej)=>{(async function tick(){try{if(!alive())return;const j=await api('GET','/ui/job/'+id);onTick(j);
    if(!j.done){setTimeout(tick,500);return}res(j)}catch(e){rej(e)}})()})}
function poll(){return waitJob(st.job,j=>{const[d,t]=j.progress;$('prog').style.width=(j.step==='hachage'?(t?d/t*100:0):100)+'%';$('prepStep').textContent=jobLine(j);stage(j.step,!!j.error)}).then(j=>{
    if(j.error){$('prepStep').innerHTML='<span class="warn-text" style="color:var(--err)">'+esc(j.error)+'</span>';return}
    st.miErr=j.mediainfo_error||'';st.analysis=j.analysis;st.torrentName=j.torrent.name;$('prep').hidden=true;upPanels(j);
    if(j.category)$('category').value=j.category;musicMode(!!j.analysis.music);go(2);
    if(st.music){musicStart(j.analysis);return}
    $('kind').value=j.analysis.guessed_type===TK.tv?TK.tv:TK.movie;showAnalysis(j.analysis);
    $('q').value=j.analysis.clean_title||j.torrent.name;$('year').value=j.analysis.year||'';$('work_title').value=j.analysis.clean_title||'';$('episode').value=$('kind').value===TK.tv?(j.episode||''):'';$('episode_title').value='';
    $('tmdb').innerHTML='';search()})}

// ---- étape 2 : œuvre et fiche ----
// Fiche partagée avec la loupe du lot : puces (lu / déclaré) et champs des facettes.
const FACET_LAB={source:'Source',edition:'Édition',group:'Team',languages:'Langues',resolution:'Résolution',video_codec:'Codec vidéo',bit_depth:'Profondeur',hdr:'HDR',audio_codec:'Codec audio',channels:'Canaux'};
const facetChips=n=>Object.entries(n.facets||{}).map(([k,v])=>`<span class="chip ${v.origin}" title="${esc(FACET_LAB[k]||k)}">${esc(v.value)}</span>`).join('')+(n.media?Object.values(n.media).filter(Boolean).map(v=>`<span class="chip">${esc(v)}</span>`).join(''):'');
// vals : valeurs imposées (sinon celles de l'analyse). Liste fermée → select, sinon champ libre.
function facetGrid(n,keys,vals={}){const voc=n.vocabulary||{};return keys.map(k=>{const v=vals[k]??n.facets?.[k]?.value??'',lab=esc(FACET_LAB[k]||k),list=voc[k]||(k==='source'||k==='edition'?[]:null);
  return list?`<label>${lab}<select data-f="${esc(k)}"><option value="">—</option>${[...new Set([...list,...(v&&!list.includes(v)?[v]:[])])].map(t=>`<option ${v===t?'selected':''}>${esc(t)}</option>`).join('')}</select></label>`:`<label>${lab}<input data-f="${esc(k)}" value="${esc(v)}"></label>`}).join('')}
const advVals=()=>{const o={};$('advFacets').querySelectorAll('[data-f][data-t]').forEach(el=>{if(el.value)o[el.dataset.f]=el.value});return o};
const facetVals=box=>{const o={};box.querySelectorAll('[data-f]').forEach(el=>{if(el.value)o[el.dataset.f]=el.value});return o};
function showAnalysis(a){const n=a.nomenclature;
  $('warnings').innerHTML=(st.miErr?msg('warn','MediaInfo : '+esc(st.miErr)+' — les facettes restent déclarées, Ratatosk les vérifiera.'):'')+(a.warnings||[]).map(w=>msg('warn',esc(w))).join('')+botMsgs(a)||msg('ok','Aucun avertissement : Ratatosk n\'a rien à redire.');
  if(!n){$('built').textContent=a.name;$('chips').innerHTML='';$('legend').hidden=true;$('facets').innerHTML='';$('missing').textContent='';return}
  $('built').textContent=n.built_name||a.name;$('missing').textContent=n.missing?.length?'Manque : '+n.missing.join(', ')+(n.missing_title?' · titre de l\'œuvre':''):(n.missing_title?'Choisis l\'œuvre (titre)':'');
  $('chips').innerHTML=facetChips(n);$('legend').hidden=!$('chips').children.length;
  if(!$('facets').children.length){$('facets').innerHTML=facetGrid(n,['source','edition','group']);
    $('facets').querySelectorAll('[data-f]').forEach(el=>el.onchange=reanalyze)}
  if(!$('advFacets').children.length){const keys=[...new Set(['languages',...Object.keys(n.facets||{})])].filter(k=>!['source','edition','group'].includes(k));
    $('advFacets').innerHTML='<label>Catégorie<select id="advCat"></select></label>'+facetGrid(n,keys);
    $('advCat').onchange=()=>{$('category').value=$('advCat').value;reanalyze()};
    $('advFacets').querySelectorAll('[data-f]').forEach(el=>el.onchange=()=>{el.dataset.t=1;reanalyze()})}
  advCatFill()}
// Sous-catégories de la famille du type TMDB (film ou série), comme dans la loupe ; la catégorie
// hors de cette famille, elle passe à celle du lot pour ce type (Série TV, Film), ou à la première sous-catégorie.
function advCatFill(){const s=$('advCat');if(!s)return;const k=$('kind').value,def=bdCat(k)||(k===TK.tv?'series':'films');s.innerHTML=catFamily(def);
  const opts=[...s.options].map(o=>o.value),v=$('category').value;
  if(opts.includes(v)){s.value=v;return}
  const nv=opts.includes(def)&&def.includes('-')?def:opts.find(o=>o.includes('-'))||opts[0];if(!nv)return;
  $('category').value=nv;s.value=nv;if(st.job&&!st.music)reanalyze()}
$('kind').addEventListener('change',advCatFill);
function sheet(){const facets={...advVals(),...facetVals($('facets'))};
  // Album : le type n'est envoyé qu'une fois touché (absent = types de l'édition MusicBrainz, vide = aucun).
  if(st.music){if(st.typesTouched)facets.type=[...$('types').querySelectorAll('.on')].map(b=>b.dataset.t).join(',');
    return{category:$('category').value,facets,musicbrainz_id:st.mbPicked?.id,release_name:st.analysis?.music?.built_name||undefined}}
  return{category:$('category').value,facets,work_title:$('work_title').value,year:$('year').value,episode:$('episode').value,episode_title:$('episode_title').value,tmdb_id:st.picked?.id,tmdb_type:st.picked?($('kind').value):undefined}}
async function reanalyze(){$('builtBox').classList.add('loading');try{const a=await api('POST','/ui/analyze',{job:st.job,...sheet()});st.analysis=a;st.music&&a.music?showMusic(a):showAnalysis(a)}catch(e){toast('Analyse : '+e.message,'err')}finally{$('builtBox').classList.remove('loading')}}
['work_title','year','episode','episode_title'].forEach(id=>$(id).onchange=reanalyze);

async function search(){$('tmdb').innerHTML='<div class="empty">Recherche sur TMDB…</div>';
  const r=await api('GET','/ui/tmdb?q='+encodeURIComponent($('q').value)+'&type='+$('kind').value).catch(e=>({results:[],error:e.message}));
  $('tmdb').innerHTML=(r.results||[]).slice(0,6).map((x,i)=>`<button type="button" class="c ${st.picked&&st.picked.id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.poster_url||'')}" alt="" loading="lazy"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc(x.overview||'')}</div></div></button>`).join('')||`<div class="empty">${r.error?esc(r.error):'Aucun résultat sur TMDB — tu peux quand même remplir le titre à la main.'}</div>`;
  [...$('tmdb').querySelectorAll('.c')].forEach(d=>d.onclick=()=>{[...$('tmdb').children].forEach(x=>x.classList.remove('sel'));d.classList.add('sel');st.picked=r.results[d.dataset.i];reveal($('facets'));$('work_title').value=st.picked.title;if(st.picked.year)$('year').value=st.picked.year;reanalyze();images()})}
onSubmit($('searchForm'),()=>run($('search'),search));
$('kind').onchange=search;
async function images(){const r=await api('GET','/ui/tmdb-images?id='+st.picked.id+'&type='+$('kind').value+'&episode='+encodeURIComponent($('episode').value)).catch(()=>({images:[]}));st.images=r.images||[];st.captures=[];
  $('imgsNote').hidden=st.images.length>0;if(!st.images.length)$('imgsNote').textContent='Aucune image TMDB pour cette œuvre.';
  $('imgs').innerHTML=st.images.map((im,i)=>`<button type="button" data-i="${i}" title="Ajouter aux captures"><img src="${esc(im.thumb)}" alt="" loading="lazy"></button>`).join('');
  [...$('imgs').children].forEach(im=>im.onclick=()=>{im.classList.toggle('sel');const u=st.images[im.dataset.i].url;st.captures=im.classList.contains('sel')?[...st.captures,u]:st.captures.filter(x=>x!==u);if(im.classList.contains('sel')&&st.step===3)insert(TAGS[$('format').value].img(u)[0]+'\n')})}

// ---- étape 2, version album (docs/25 §6) ----
// Étape 2 : arborescence (un dossier seulement) et MediaInfo de la préparation, repliées comme dans la loupe.
function upPanels(j){st.up={job:j.id||st.job,miErr:j.mediainfo_error||'',mi:null};const t=$('upTree'),ft=t.querySelector('.ft'),m=$('upMi');
  t.hidden=!(j.main_file&&j.main_file!==j.path);ft.dataset.path=j.path;delete ft.dataset.done;ft.innerHTML='';if(t.open&&!t.hidden)ftLoad(ft);
  m.querySelector('.mib').innerHTML='';m.querySelector('summary .sz').textContent='';miShow(m,st.up)}
$('upTree').addEventListener('toggle',()=>{if($('upTree').open)ftLoad($('upTree').querySelector('.ft'))});
$('upMi').addEventListener('toggle',()=>miShow($('upMi'),st.up));
function musicMode(on){st.music=on;$('upAdv').hidden=on;$('videoWork').hidden=on;$('videoFields').hidden=on;$('musicWork').hidden=!on;$('types').hidden=!on;$('tracks').hidden=!on;
  $('p2title').textContent=on?'L\'album et la fiche technique':'L\'œuvre et la fiche technique'}
// Ratatosk parle en block / warn / info.
function botMsgs(a,skip=[]){return(a.bot||[]).filter(b=>!skip.includes(b.code)).map(b=>msg(b.level==='block'?'err':b.level==='warn'?'warn':'info','Ratatosk : '+esc(b.message))).join('')}
function musicStart(a){const m=a.music;$('types').innerHTML='';$('tracks').innerHTML='';$('mb').innerHTML='';
  // Tags Picard : l'édition est déjà connue, la grille la montre sélectionnée.
  if(m.musicbrainz_id)st.mbPicked={id:m.musicbrainz_id,tags:true};
  showMusic(a);$('mbArtist').value=m.artist||'';$('mbAlbum').value=m.album||'';
  if(m.album)mbSearch();else $('mb').innerHTML='<div class="empty">Pas de tag « album » dans les pistes : tape l\'artiste et l\'album pour chercher l\'édition.</div>'}
function showMusic(a){const m=a.music,voc=m.vocabulary||{};
  $('warnings').innerHTML=(st.miErr?msg('warn','MediaInfo : '+esc(st.miErr)):'')+(m.probe_error?msg('err',esc(m.probe_error)):'')
    +(!m.sheet&&!m.probe_error?msg('warn','Pistes non lues : sans rapport MediaInfo, la release garde le nom du dossier et un NFO est à joindre.'):'')
    +(m.rip_log?msg('info','Un .log ou un .cue accompagne les pistes : source CD déduite.'):'')
    +(st.mbPicked?.tags&&st.mbPicked.id===m.musicbrainz_id?msg('info','Édition MusicBrainz lue dans les tags des pistes.'):'')
    +(m.issues||[]).map(i=>msg('warn',esc(i.message))).join('')+(a.warnings||[]).map(w=>msg('warn',esc(w))).join('')
    // Les espaces du dossier ne comptent pas : c'est le nom calculé qui sera publié.
    +botMsgs(a,m.built_name?['naming_spaces']:[])||msg('ok','Aucun avertissement : Ratatosk n\'a rien à redire.');
  $('built').textContent=m.built_name||st.torrentName||a.name;$('legend').hidden=true;
  $('missing').textContent=m.missing?.length?'Manque : '+m.missing.join(', ')+' — sans quoi la release garde le nom de son dossier.':'';
  $('chips').innerHTML=[m.format_label,m.source,...(m.types||[]),m.discs>1?m.discs+' disques':'',m.track_count?m.track_count+' pistes':'',m.duration,m.label,m.year].filter(Boolean).map(v=>`<span class="chip">${esc(v)}</span>`).join('');
  $('tracks').innerHTML=(m.tracks||[]).map((t,i)=>`<li><span class="mut">${m.discs>1?t.disc+'-':''}${String(t.number??i+1).padStart(2,'0')}</span><span>${esc(t.title)}</span><span class="mut d">${esc(t.duration)}</span></li>`).join('');
  if(!$('facets').children.length){
    $('facets').innerHTML=`<label>Source<select data-f="source"><option value="">—</option>${(voc.source||[]).map(s=>`<option ${m.source===s?'selected':''}>${esc(s)}</option>`).join('')}</select></label><label>Team<input data-f="group" value="${esc(m.group||'')}"></label>`;
    $('facets').querySelectorAll('[data-f]').forEach(el=>el.onchange=reanalyze)}
  $('types').innerHTML='<span class="lbl" title="Pré-rempli depuis l\'édition MusicBrainz">Type</span>'+(voc.type||[]).map(t=>{const on=(m.types||[]).includes(t);return`<button type="button" data-t="${esc(t)}" class="${on?'on':''}" aria-pressed="${on}">${esc(t)}</button>`}).join('')}
$('types').addEventListener('click',e=>{const b=e.target.closest('[data-t]');if(!b)return;b.setAttribute('aria-pressed',b.classList.toggle('on'));st.typesTouched=true;reanalyze()});
async function mbSearch(){const m=st.analysis?.music||{};$('mb').innerHTML='<div class="empty">Recherche sur MusicBrainz…</div>';
  const q=new URLSearchParams({artist:$('mbArtist').value,album:$('mbAlbum').value});if(m.track_count)q.set('tracks',m.track_count);
  const r=await api('GET','/ui/musicbrainz?'+q).catch(e=>({results:[],error:e.message}));st.mbResults=r.results||[];
  $('mb').innerHTML=st.mbResults.map((x,i)=>`<button type="button" class="c ${st.mbPicked?.id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.cover_url||'')}" alt="" loading="lazy" onerror="this.style.visibility='hidden'"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc([x.artist,x.label,x.country,x.media,x.disambiguation].filter(Boolean).join(' · '))}</div>${x.tracks_match?`<span class="bdg seed">${x.track_count} pistes, comme ton dossier</span>`:`<span class="bdg x">${x.track_count} pistes</span>`}</div></button>`).join('')
    ||`<div class="empty">${r.error?esc(r.error):'Aucune édition sur MusicBrainz — le nom viendra des tags des pistes.'}</div>`;
  [...$('mb').querySelectorAll('.c')].forEach(d=>d.onclick=()=>{const x=st.mbResults[d.dataset.i];const off=st.mbPicked?.id===x.id&&!st.mbPicked.tags;
    [...$('mb').children].forEach(c=>c.classList.remove('sel'));if(off)st.mbPicked=null;else{d.classList.add('sel');st.mbPicked=x}reanalyze()})}
onSubmit($('mbForm'),()=>run($('mbSearch'),mbSearch));

// ---- étape 3 : présentation ----
$('toPres').onclick=()=>run($('toPres'),async()=>{if(!st.templates.length){const c=await api('GET','/ui/presentations').catch(()=>({templates:[]}));st.templates=c.templates||[]}
  const def=presDefault($('category').value.split('-')[0]);
  $('tpl').innerHTML='<option value="">— aucun (description libre) —</option>'+st.templates.map((t,i)=>`<option value="${i}" ${t===def?'selected':''}>${esc(t.name)}${t.site?' · Draupnirr':''}${t.family?' · '+esc(t.family):''}</option>`).join('');
  $('capsBox').hidden=st.music;go(3);if(!$('description').value)regen();fillVars();schedulePreview()});
// Le modèle par défaut, comme presDefault() du site (docs/25 §4.2) : celui du membre pour la famille, puis
// toutes catégories ; s'il n'a AUCUN modèle pour la famille, celui du site. Des modèles sans défaut = à la main.
function presDefault(fam){const c=st.templates.filter(t=>!t.family||t.family===fam),own=c.filter(t=>!t.site);
  if(own.length)return own.find(t=>t.is_default&&t.family===fam)||own.find(t=>t.is_default&&!t.family)||null;
  const site=c.filter(t=>t.site&&t.family===fam);return site.find(t=>t.is_default)||site[0]||null}
// {{pistes}} : liste numérotée selon le format du modèle.
function pistes(tracks,fmt){const it=(tracks||[]).map(t=>(fmt==='html'?esc(t.title):t.title)+(t.duration?' ('+t.duration+')':''));
  return!it.length?'':fmt==='html'?'<ol><li>'+it.join('</li><li>')+'</li></ol>':'[list=1]\n[*]'+it.join('\n[*]')+'\n[/list]'}
function vars(){const v=videoVars(),a=st.analysis||{},al=a.music;if(!al)return v;
  // Album (docs/25 §6.7) : titre, affiche et durée aussi, pour qu'un modèle film reste lisible.
  return{...v,titre:al.album||'',annee:String(al.year||''),type:'Album',affiche:al.cover_url||'',nom_release:al.built_name||a.name||'',
    source:al.source||'',team:al.group||'',duree:al.duration||'',nfo:al.nfo||'',tags:(al.tags||[]).join(', '),episode:'',titre_episode:'',
    artiste:al.artist||'',album:al.album||'',label:al.label||'',format_audio:al.format_label||'',pistes:pistes(al.tracks,$('format').value),
    nb_pistes:String(al.track_count||''),musicbrainz_url:al.musicbrainz_url||''}}
function videoVars(){const a=st.analysis||{},n=a.nomenclature||{},f=n.facets||{},v=k=>f[k]?.value||'',m=n.media||{},fmt=$('format').value,p=st.picked||{};
  const caps=st.captures.map(u=>fmt==='html'?`<img src="${u}" alt="">`:`[img]${u}[/img]`).join('\n');
  return{titre:p.title||a.clean_title||'',annee:$('year').value,type:$('kind').value===TK.tv?'Série':'Film',synopsis:p.overview||'',affiche:p.poster_url||'',tmdb_url:p.id?`https://www.themoviedb.org/${$('kind').value}/${p.id}`:'',nom_release:n.built_name||a.name||'',taille:a.size_human||'',nb_fichiers:String(a.file_count||''),fichiers:(a.files||[]).map(x=>`${x.path} (${x.size_human})`).join('\n'),episode:$('episode').value,titre_episode:$('episode_title').value,tags:(a.suggested_tags||[]).join(', '),source:v('source'),edition:v('edition'),team:v('group'),langues:v('languages'),resolution:v('resolution'),codec_video:v('video_codec'),profondeur:v('bit_depth'),hdr:v('hdr'),codec_audio:v('audio_codec'),canaux:v('channels'),duree:m.duration||'',debit:m.bitrate||'',sous_titres:m.subtitles||'',mediainfo:'',nfo:n.nfo||'',captures:caps,uploadeur:$('anonymous').checked?'Anonyme':(st.me?.name||''),date:new Date().toLocaleDateString('fr-FR')}}
function render(body,d){let out=body;for(;;){const s=out.indexOf('{{#');if(s<0)break;const e=out.indexOf('}}',s);const name=out.slice(s+3,e);const close='{{/'+name+'}}';const c=out.indexOf(close,s);if(c<0)break;out=out.slice(0,s)+(d[name]?out.slice(e+2,c):'')+out.slice(c+close.length)}
  return out.replace(/\{\{\s*([a-z_]+)\s*\}\}/g,(m,k)=>d[k]??'').replace(/[ \t]+$/gm,'').replace(/\n{3,}/g,'\n\n').trim()}
function regen(){const t=st.templates[$('tpl').value];if(!t){return}$('format').value=t.format;$('description').value=render(t.body,vars());schedulePreview()}
$('regen').onclick=regen;$('tpl').onchange=regen;

// ---- éditeur assisté : balises autour de la sélection, selon le format ----
const TAGS={
  bbcode:{b:['[b]','[/b]'],i:['[i]','[/i]'],u:['[u]','[/u]'],h2:['[h2]','[/h2]'],size:v=>[`[size=${v}]`,'[/size]'],color:v=>[`[color=${v}]`,'[/color]'],center:['[center]','[/center]'],quote:['[quote]','[/quote]'],list:['[list]\n[*]','\n[/list]'],item:['[*]',''],link:u=>[`[url=${u}]`,'[/url]'],img:u=>[`[img]${u||'URL'}[/img]`,''],code:['[code]','[/code]'],spoiler:['[spoiler]','[/spoiler]'],hr:['[hr]\n','']},
  html:{b:['<b>','</b>'],i:['<i>','</i>'],u:['<u>','</u>'],h2:['<h2>','</h2>'],size:v=>[`<span style="font-size:${v}%">`,'</span>'],color:v=>[`<span style="color:${v}">`,'</span>'],center:['<center>','</center>'],quote:['<blockquote>','</blockquote>'],list:['<ul>\n<li>','</li>\n</ul>'],item:['<li>','</li>'],link:u=>[`<a href="${u}">`,'</a>'],img:u=>[`<img src="${u||'URL'}">`,''],code:['<code>','</code>'],spoiler:['<details><summary>Spoiler</summary>','</details>'],hr:['<hr>\n','']}};
const ta=$('description');
// wrap : entoure la sélection ; sans sélection, le curseur reste dans la balise ouverte.
function wrap([before,after],sel){const t=ta;const s=sel?sel[0]:t.selectionStart,e=sel?sel[1]:t.selectionEnd;const mid=t.value.slice(s,e);
  t.value=t.value.slice(0,s)+before+mid+after+t.value.slice(e);t.focus();
  if(mid)t.setSelectionRange(s+before.length,s+before.length+mid.length);else t.setSelectionRange(s+before.length,s+before.length);schedulePreview()}
function insert(text){wrap([text,''])}
function cmd(k,arg){const d=TAGS[$('format').value][k];const sel=st.edSel;st.edSel=null;wrap(typeof d==='function'?d(arg):d,sel)}
$('tb').addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;
  if(b.dataset.cmd){cmd(b.dataset.cmd);return}
  st.edSel=[ta.selectionStart,ta.selectionEnd];pop(b.dataset.pop)});
ta.addEventListener('keydown',e=>{if(!(e.metaKey||e.ctrlKey))return;const k={b:'b',i:'i',u:'u'}[e.key.toLowerCase()];if(k){e.preventDefault();cmd(k)}});
const SWATCHES=['#E04848','#E8A33D','#3FAE5C','#3E8ED0','#9B6BD6','#8A94A6'];
function pop(kind){const p=$('pop');if(p.dataset.kind===kind&&!p.hidden){closePop();return}p.dataset.kind=kind;p.hidden=false;
  if(kind==='color')p.innerHTML=SWATCHES.map(c=>`<button type="button" class="sw" data-v="${c}" style="background:${c}" title="${c}"></button>`).join('');
  else if(kind==='size')p.innerHTML=[['90','Petit'],['130','Grand'],['200','Très grand']].map(([v,l])=>`<button type="button" data-v="${v}">${l}</button>`).join('');
  else{p.innerHTML=`<input type="url" placeholder="${kind==='img'?'https://…/image.jpg':'https://…'}" autocomplete="off"><button type="button" class="primary" data-ok="1">Insérer</button>`;p.querySelector('input').focus()}}
function closePop(){$('pop').hidden=true;$('pop').dataset.kind=''}
$('pop').addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;const k=$('pop').dataset.kind;
  if(b.dataset.v){cmd(k,b.dataset.v);closePop();return}
  const u=$('pop').querySelector('input').value.trim();if(!u)return;cmd(k,u);closePop()});
$('pop').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('pop').querySelector('[data-ok]')?.click()}if(e.key==='Escape')closePop()});
document.addEventListener('click',e=>{if(!$('pop').hidden&&!e.target.closest('#pop')&&!e.target.closest('[data-pop]'))closePop()});
// Palette de variables : la VALEUR rendue est insérée, le modèle reste sur le site.
function fillVars(){const v=vars();$('varSel').innerHTML='<option value="">Insérer une variable…</option>'+Object.keys(v).map(k=>`<option value="${k}">${k} — ${esc(String(v[k]).replace(/\s+/g,' ').slice(0,40))||'(vide)'}</option>`).join('')}
$('varSel').onchange=()=>{const k=$('varSel').value;if(k)insert(String(vars()[k]||''));$('varSel').value=''};
['year','episode','episode_title','anonymous','format'].forEach(id=>$(id).addEventListener('change',fillVars));

// ---- aperçu en temps réel : debounce 500 ms, réponse périmée ignorée ----
let pvTimer=0,pvSeq=0;
function schedulePreview(){clearTimeout(pvTimer);$('pvState').textContent='rendu…';pvTimer=setTimeout(renderPreview,500)}
async function renderPreview(){const seq=++pvSeq;const content=ta.value;if(!content.trim()){$('preview').innerHTML='';$('pvState').textContent='';return}
  try{const r=await api('POST','/ui/preview',{content,format:$('format').value});if(seq!==pvSeq)return;$('preview').innerHTML=r.html;$('pvState').textContent=''}
  catch(e){if(seq!==pvSeq)return;$('pvState').textContent='aperçu indisponible : '+e.message}}
ta.addEventListener('input',schedulePreview);
$('format').addEventListener('change',schedulePreview);
$('editor').querySelectorAll('.tabs button').forEach(b=>b.onclick=()=>{$('editor').dataset.tab=b.dataset.tab;$('editor').querySelectorAll('.tabs button').forEach(x=>x.classList.toggle('on',x===b))});

// ---- étape 4 : publication ----
$('publish').onclick=()=>run($('publish'),async()=>{
  const al=st.analysis?.music;
  // Album : le serveur reprend nom, NFO, pochette, titre et tags de la fiche et de l'édition.
  const meta=al?{musicbrainz_id:st.mbPicked?.id||al.musicbrainz_id||undefined,facets:sheet().facets}:{...sheet(),poster_url:st.picked?.poster_url,synopsis:st.picked?.overview,tags:(st.analysis?.suggested_tags||[])};
  const r=await api('POST','/ui/publish',{job:st.job,category:$('category').value,description:$('description').value,description_format:$('format').value,nfo_text:$('nfo').value,meta});
  $('publish').disabled=true;$('publish').classList.remove('busy');
  $('pubTitle').textContent=r.result.awaiting_validation?'Publié, en attente de Ratatosk':'Publié au catalogue';
  $('pub').innerHTML=msg('ok',r.result.awaiting_validation?'<b>En attente de validation Ratatosk</b> — garde le seed, la release passe au catalogue dès qu\'elle est vérifiée.':'<b>Au catalogue</b> — la release est visible par les membres.')
    +(r.client_added?msg('ok',`Ajouté à ${esc(r.client_added)} sur les mêmes données${$('cl_skip').checked?', sans re-vérification':''}.`):'')
    +(r.client_error?msg('warn','Client torrent : '+esc(r.client_error)):'')
    +(r.saved?msg('ok','.torrent personnalisé enregistré : <span class="mono">'+esc(r.saved)+'</span>'+(r.client_added?'':' — ajoute-le à ton client sur les mêmes données.')):msg('warn',esc(r.download_error||'Le .torrent personnalisé n\'a pas pu être récupéré.')));
  $('pubLink').href=r.url;go(4);});
$('again').onclick=()=>{st.job=null;st.analysis=null;st.picked=null;st.mbPicked=null;st.typesTouched=false;musicMode(false);$('mb').innerHTML='';st.captures=[];st.images=[];st.sel=null;st.hash=null;st.max=1;
  $('facets').innerHTML='';$('description').value='';$('nfo').value='';$('preview').innerHTML='';$('imgs').innerHTML='';$('tmdb').innerHTML='';$('pub').innerHTML='';$('publish').disabled=false;$('prepare').disabled=true;
  go(1);if(st.mode==='client')fromClient();else browse($('path').value).catch(e=>toast(e.message,'err'))};

load().catch(e=>{$('connect').hidden=false;$('connectErr').textContent=e.message});


// ---- Vue Cross-seed ----
function view(name){const cross=name==='cross',batch=name==='batch',up=!cross&&!batch;$('viewCross').classList.toggle('on',cross);$('viewBatch').classList.toggle('on',batch);$('viewUpload').classList.toggle('on',up);
  document.querySelector('.bridge').hidden=!up;[1,2,3,4].forEach(k=>$('p'+k).hidden=!up||k!==st.step);$('pX').hidden=!cross;$('pB').hidden=!batch;if(batch)batchInit();scrollTop()}
$('viewUpload').onclick=()=>view('upload');$('viewCross').onclick=()=>view('cross');$('viewBatch').onclick=()=>view('batch');
st.cross=[];
function crossRow(r){const m=r.match;const key=esc(r.hash);
  return `<div class="x ${m?'':'none'}" data-h="${key}"><span class="nm" title="${esc(r.path)}"><b>${esc(r.name)}</b><small>${esc(r.path)}</small></span><span class="r">${human(r.size)}</span>`+
    (m?`<span class="nm"><b>${esc(m.name)}</b><small>${esc(m.category||'')} · ${m.seeders??0} seeder(s) · ${Math.round((r.score||0)*100)} % de ressemblance</small></span><span><button type="button" class="primary small" data-add="${key}"><svg class="i"><use href="#i-link"/></svg>Cross-seeder</button></span>`
      :`<span class="mut">aucune release de cette taille sur Draupnirr</span><span></span>`)+`</div>`}
function renderCross(){const only=$('crossOnly').checked;const rows=st.cross.filter(r=>!only||r.match);const n=st.cross.filter(r=>r.match).length;
  $('crossCount').textContent=st.cross.length?`${n} correspondance(s) sur ${st.cross.length} torrent(s) hors Draupnirr`:'';
  $('crossAll').hidden=n===0;$('crossAll').innerHTML=`${ico('link')}Tout cross-seeder (${n})`;$('crossOnlyWrap').hidden=st.cross.length===0;
  $('crossList').innerHTML='<div class="x h"><span>Dans ton client</span><span class="r">Taille</span><span>Sur Draupnirr</span><span></span></div>'+(rows.map(crossRow).join('')||'<div class="empty"><span>Rien à afficher.</span></div>');
  [...$('crossList').querySelectorAll('[data-add]')].forEach(b=>b.onclick=()=>crossAdd(b.dataset.add,b))}
async function crossAdd(hash,btn){const r=st.cross.find(x=>x.hash===hash);if(!r||!r.match)return;const row=btn.closest('.x');const cell=btn.parentElement;
  await run(btn,async()=>{try{const res=await api('POST','/ui/crossseed/add',{path:r.path,id:r.match.id});r.done=true;cell.innerHTML=`<span class="ok">${ico('check')} ajouté au client</span>`;toast('Cross-seed ajouté : '+res.name,'ok')}
    catch(e){const msg=(e.problems||[]).join(' · ')||e.message;cell.innerHTML=`<span class="err" title="${esc(msg)}">${ico('alert')} refusé</span>`;row.title=msg;toast(msg,'err')}})}
$('crossScan').onclick=()=>run($('crossScan'),async()=>{$('crossList').innerHTML='<div class="empty"><span>Lecture du client et comparaison avec Draupnirr…</span></div>';
  try{const r=await api('GET','/ui/crossseed/scan');st.cross=(r.rows||[]).map(x=>({...x,done:false}));renderCross();toast(`${r.matched} correspondance(s) trouvée(s)`,r.matched?'ok':'info')}
  catch(e){st.cross=[];renderCross();$('crossList').innerHTML=`<div class="empty"><span>${esc(e.message)}</span></div>`;toast(e.message,'err')}});
$('crossOnly').onchange=renderCross;
$('crossAll').onclick=()=>run($('crossAll'),async()=>{const todo=st.cross.filter(r=>r.match&&!r.done);let ok=0,ko=0;
  for(const r of todo){const btn=$('crossList').querySelector(`[data-add="${CSS.escape(r.hash)}"]`);if(!btn)continue;await crossAdd(r.hash,btn);r.done?ok++:ko++}
  toast(`Cross-seed terminé : ${ok} ajouté(s), ${ko} refusé(s)`,ko?'warn':'ok')});


// ---- Vue Lot ----
st.batchTimer=null;st.batchKind='video';st.batchTab='*';st.batchSort=null;st.batchJob=null;st.batchPick=null;st.batchOffSet=null;st.batchSel=[];
function batchInit(){if(!$('batchPath').value)$('batchPath').value=$('path').value||'';
  const cats=[...$('category').options].map(o=>`<option value="${esc(o.value)}">${esc(o.textContent)}</option>`).join('');
  if(!$('batchCatFilm').options.length){$('batchCatFilm').innerHTML=cats;$('batchCatTV').innerHTML=cats;$('batchCatFilm').value='films-film';$('batchCatTV').value='series-serie-tv'}
  batchMode();batchPoll();if($('batchPath').value&&!st.batchPick)batchPickLoad().catch(()=>{})}
// États d'une ligne du lot : les libellés que le serveur envoie (rowStatus dans batch.go).
const BS=Object.freeze({wait:'attente',run:'en cours',pub:'publié',sim:'simulé',rev:'à revoir',rv:'revu',err:'erreur',present:'déjà présent',ign:'ignoré'});
// Un onglet par état, dans cet ordre ; '' = pas encore examinée. Publié est épinglé à droite.
const BTABS=[{k:'',lab:'Nouveau',ic:'st-new'},{k:BS.run,lab:'En cours',ic:'st-run',c:'run'},{k:BS.wait,lab:'En attente',ic:'st-wait',c:'wait'},
  {k:BS.sim,lab:'Simulé',ic:'st-sim',c:'sim'},{k:BS.rev,lab:'À revoir',ic:'eye',c:'rev'},{k:BS.rv,lab:'Revu',ic:'st-rv',c:'rv'},{k:BS.err,lab:'Erreur',ic:'alert',c:'err'},
  {k:BS.present,lab:'Déjà présent',ic:'st-dup',c:'run'},{k:BS.ign,lab:'Ignoré',ic:'st-skip',c:'wait'},{k:BS.pub,lab:'Publié',ic:'st-pub',c:'pub'}];
const bTab=s=>BTABS.find(t=>t.k===(s||''));
const bRank=s=>BTABS.findIndex(t=>t.k===(s||''));
const bBadge=s=>bTab(s)?.c||'wait';
const bPub=r=>r.status===BS.pub;
function renderBatch(j){st.batchJob=j;const rows=j.rows||[];
  if(rows.length||j.running){$('batchSummary').hidden=false;
    const todo=rows.filter(r=>r.status!==BS.present),done=todo.filter(r=>r.status!==BS.wait&&r.status!==BS.run).length,cur=rows.find(r=>r.status===BS.run);
    const total=j.limit>0?Math.min(j.limit,todo.length):todo.length;const pct=total?Math.round(done/total*100):0;
    $('batchProg').style.width=pct+'%';$('batchPct').textContent=`${done} / ${total} · ${pct} %`;
    $('batchNow').textContent=cur?`En cours : ${cur.name} — ${cur.detail||''}`:(j.running?'Préparation…'+(j.note||''):(j.done?'Terminé.':''));
    $('batchCount').textContent=`${rows.length} élément(s) · ${j.skipped||0} déjà présent(s) · ${j.published||0} ${j.dry_run?'publiable(s)':'publié(s)'} · ${j.review||0} à revoir`+(j.reviewed?` · ${j.reviewed} revue(s)`:'')+(j.running?' · en cours…':j.done?' · terminé':'')+(j.error?' · '+j.error:'')}
  $('batchStop').hidden=!j.running;
  renderBatchTable()}
// Dernière décision connue (historique), datée, tant que le lot ne l'a pas remplacée.
function lastSeen(l){if(!l)return {};const d=new Date(l.at).toLocaleDateString('fr-FR',{day:'2-digit',month:'2-digit'});
  return {status:l.status,detail:`${d}${l.detail?' · '+l.detail:''}`,url:l.url||'',built:l.built,tmdb:l.tmdb,edition:l.edition}}
// Déjà sur Draupnirr d'après l'historique : décochée par défaut (le lot la sauterait de toute façon).
const bDone=l=>!!l&&(l.status===BS.pub||l.status===BS.present);
// Les lignes du tableau : les entrées du dossier listé, avec l'état du lot de cette session s'il les a vues,
// sinon la dernière décision de l'historique. Une entrée décochée garde son état : elle reste dans son onglet.
function batchRows(){const j=st.batchJob||{},jobRows=j.rows||[],running=!!j.running,pk=st.batchPick,
    // Un lot qui tourne sur un autre dossier garde son tableau ; la liste à cocher revient quand il s'arrête.
    p=pk&&pk.key===batchPickKey()&&!(running&&j.root!==pk.path)?pk:null;
  if(!p)return {p,running,rows:jobRows};
  const byPath=new Map(jobRows.map(r=>[r.path,r]));
  return {p,running,rows:p.entries.map(e=>{const r=byPath.get(e.path),live=r&&(running||(r.status!==BS.wait&&r.status!==BS.run))?r:null;
    return {name:e.name,size:e.size,path:e.path,is_dir:e.is_dir,choice:e.choice,episode:e.episode||'',status:'',...lastSeen(e.last),...(live?{...live,detail:live.detail||''}:{}),on:p.on.has(e.path),pick:true}})}}
// Recherche sans casse ni accents, sur le nom du dossier et le nom calculé.
const bNorm=t=>String(t||'').normalize('NFD').replace(/[̀-ͯ]/g,'').toLowerCase();
const bQ=()=>bNorm($('batchQ').value.trim());
const bHl=t=>{const k=bQ(),i=k?bNorm(t).indexOf(k):-1;return i<0?esc(t):esc(t.slice(0,i))+'<mark>'+esc(t.slice(i,i+k.length))+'</mark>'+esc(t.slice(i+k.length))};
const bSub=r=>[r.built||r.tmdb,r.edition&&'MusicBrainz : '+r.edition].filter(Boolean).join(' · ');
function renderBatchTabs(rows,running){const n=new Map();rows.forEach(r=>n.set(r.status||'',(n.get(r.status||'')||0)+1));
  // Pendant un lot, « En attente » reste ouvert même vide : il se vide au fil du lot, la fin ouvre le résultat.
  if(st.batchTab!=='*'&&st.batchTab!==BS.pub&&!n.get(st.batchTab)&&!(running&&st.batchTab===BS.wait)){st.batchTab='*';st.batchOffSet=null}
  // Picto et nombre ; l'onglet ouvert déplie aussi son nom.
  const t=(k,lab,ic,c,cnt)=>{const on=st.batchTab===k;return `<button type="button" role="tab" data-t="${esc(k)}" class="${on?'on':''}" aria-selected="${on}" title="${esc(lab)} · ${cnt}" aria-label="${esc(lab)}, ${cnt}"><span class="pil ${c||''}">${ico(ic)}${on?`<span class="lab">${esc(lab)}</span>`:''}<span class="cnt">${cnt}</span></span></button>`};
  const html='<div class="scroll">'+t('*','Tout','list','',rows.length)+BTABS.filter(x=>x.k!==BS.pub&&(n.get(x.k)||x.k===st.batchTab)).map(x=>t(x.k,x.lab,x.ic,x.c,n.get(x.k)||0)).join('')+
    '</div><div class="pinned">'+t(BS.pub,'Publié','st-pub','pub',n.get(BS.pub)||0)+'</div>';
  // Réécrits seulement s'ils changent : le suivi du lot (toutes les 2 s) ne relance ni l'animation ni le défilement.
  if(html===st.batchTabsHtml)return;st.batchTabsHtml=html;const sc=$('batchTabs').querySelector('.scroll')?.scrollLeft||0;
  $('batchTabs').innerHTML=html;$('batchTabs').querySelector('.scroll').scrollLeft=sc;
  $('batchTabs').querySelectorAll('[data-t]').forEach(b=>b.onclick=()=>{st.batchTab=b.dataset.t;st.batchOffSet=null;renderBatchTable()})}
function renderBatchTable(){batchIntroFit();const {p,running,rows}=batchRows(),dry=$('batchDry').checked;
  $('batchTabs').hidden=$('batchTools').hidden=!rows.length;
  if(!rows.length){$('batchList').classList.remove('all');$('batchList').innerHTML=`<div class="empty">${ico('folder')}<span>${p?'Ce dossier est vide.':'Choisis un dossier : ses entrées s\'affichent ici, à cocher.'}</span></div>`;
    $('batchPickCount').textContent=p?'Rien à traiter dans ce dossier.':'Liste le dossier pour choisir ce qui entre dans le lot.';st.batchSel=[];
    $('batchStartLab').textContent=dry?'Simuler':'Publier';$('batchStart').disabled=running||!!p;return}
  renderBatchTabs(rows,running);
  const tab=st.batchTab,ro=tab===BS.pub,all=tab==='*',k=bQ(),off=$('batchOffOnly').checked&&!ro;
  const scope=rows.filter(r=>(all||(r.status||'')===tab)&&(!k||bNorm(r.name+' '+bSub(r)).includes(k)));
  // « Décochées seulement » fige ses lignes : en recocher une ne la fait pas disparaître sous le doigt.
  if(off&&!st.batchOffSet)st.batchOffSet=new Set(scope.filter(r=>r.pick&&!r.on&&!bPub(r)).map(r=>r.path));
  const vis=off?scope.filter(r=>st.batchOffSet.has(r.path)):scope,sel=vis.filter(r=>r.pick&&!bPub(r)),on=sel.filter(r=>r.on).length;
  st.batchSel=sel.filter(r=>r.on).map(r=>r.path);
  $('batchOffOnly').closest('label').hidden=ro;
  $('batchOffLab').textContent=`Décochées (${scope.filter(r=>r.pick&&!r.on&&!bPub(r)).length})`;
  // Lancer ne traite que les lignes cochées affichées : l'onglet ouvert, la recherche.
  $('batchPickCount').innerHTML=running?'Lot en cours : les cases reviennent quand il s\'arrête.':ro?`<b>${vis.length} release(s) publiée(s)</b>. Rien à recocher ici : chaque ligne mène à sa page sur Draupnirr.`
    :p?`<b>${on} cochée(s) sur ${sel.length}</b>`:'Liste le dossier pour choisir ce qui entre dans le lot.';
  $('batchStartLab').textContent=`${dry?'Simuler':'Publier'}${p?' '+on:''}`;$('batchStart').hidden=ro;$('batchStart').disabled=running||(!!p&&!on);
  const so=st.batchSort,sorted=so?[...vis].sort((a,b)=>so.d*(so.k==='size'?a.size-b.size:so.k==='status'?bRank(a.status)-bRank(b.status)||a.name.localeCompare(b.name):a.name.localeCompare(b.name))):vis;
  const h=(...a)=>sortHead(so,...a),master=p&&!running&&!ro&&sel.length;
  // Revue en série : la loupe suit l'ordre affiché. Le groupe porte sur les lignes cochées.
  st.batchVis=sorted.map(r=>r.path);const rq=sorted.filter(batchCanChoose);
  $('batchRun').hidden=ro||!rq.length;$('batchRunLab').textContent=`Revoir à la suite (${rq.length})`;
  // Groupe : les releases cochées parmi celles affichées.
  const gq=rq.filter(r=>r.on);$('batchGroup').hidden=gq.length<2||st.batchKind==='music';$('batchGroupLab').textContent=`Une œuvre pour ces ${gq.length}`;
  $('batchList').classList.toggle('all',all);
  $('batchList').innerHTML=(vis.length?`<div class="b h">${master?'<input type="checkbox" id="batchMaster" aria-label="Cocher les lignes affichées">':'<span></span>'}${h('name','Release<small>dossier → nom Draupnirr</small>')}${h('size','Taille','justify-content:flex-end')}${all?`<span class="st">${h('status','État')}</span>`:''}<span class="eyebrow det">Détail</span><span></span></div>`:`<div class="empty"><span>${k?'Aucune release ne correspond à « '+esc($('batchQ').value.trim())+' » ici.':'Rien dans cet onglet.'}</span></div>`)+
    sorted.map(r=>{const sub=bSub(r),lock=bPub(r);return `<div class="b ${r.pick&&!r.on&&!lock?'off':''} ${r.pick&&!running&&!lock?'pk':''}" data-p="${esc(r.path)}">${lock?`<span class="lock" title="Publié : ni republié ni resimulé">${ico('check')}</span>`:r.pick?`<input type="checkbox" ${r.on?'checked':''} ${running?'disabled':''} aria-label="Inclure ${esc(r.name)}">`:'<span></span>'}`+
      `<span class="nm" title="${esc(r.path)}"><b>${ico(r.is_dir===false?'file':'folder')}<span class="t">${bHl(r.name)}</span></b>${sub?`<small>${ico('st-built')}<span class="t">${bHl(sub)}</span></small>`:''}</span><span class="r">${human(r.size)}</span>`+
      (all?`<span class="st">${r.status?`<span class="bdg ${bBadge(r.status)}">${esc(r.status)}</span>`:'<span class="mut dash">—</span>'}</span>`:'')+
      `<span class="det">${r.url?`<a href="${esc(r.url)}" target="_blank" rel="noopener">${esc(r.detail||r.url)}</a>`:esc(r.detail||'')||'<span class="mut">pas encore examinée</span>'}</span><button type="button" class="zoom" title="Détails${[BS.rev,BS.sim,BS.rv,''].includes(r.status||'')?' et choix de l\'œuvre':''}" aria-label="Détails de ${esc(r.name)}">${ico('search')}</button></div>`}).join('');
  const m=$('batchMaster');if(m){m.checked=on===sel.length;m.indeterminate=on>0&&on<sel.length;m.onchange=()=>{sel.forEach(r=>m.checked?p.on.add(r.path):p.on.delete(r.path));renderBatchTable()}}
  $('batchList').querySelectorAll('.b[data-p]').forEach(row=>{const c=row.querySelector('input[type=checkbox]'),path=row.dataset.p;
    row.querySelector('.zoom').onclick=()=>bqOpen(st.batchVis,path);
    if(!c||!row.classList.contains('pk'))return;
    c.onchange=()=>{c.checked?p.on.add(path):p.on.delete(path);renderBatchTable()};
    // Toute la ligne coche ou décoche, sauf un clic sur la case, un lien ou la loupe.
    row.onclick=e=>{if(e.target.closest('input,a,button'))return;c.checked=!c.checked;c.onchange()}});
  $('batchList').querySelectorAll('.h button[data-k]').forEach(b=>b.onclick=()=>{st.batchSort=st.batchSort&&st.batchSort.k===b.dataset.k?{k:b.dataset.k,d:-st.batchSort.d}:{k:b.dataset.k,d:1};renderBatchTable()})}
const bqChoosable=()=>{const rows=batchRows().rows;return (st.batchVis||[]).filter(p=>{const r=rows.find(x=>x.path===p);return r&&batchCanChoose(r)})};
$('batchRun').onclick=()=>{const ps=bqChoosable();if(ps.length)bqOpen(ps,ps[0])};
$('batchGroup').onclick=()=>{const on=st.batchPick?.on;batchGroupOpen(bqChoosable().filter(p=>on?.has(p)))};
$('batchQ').oninput=()=>{st.batchOffSet=null;renderBatchTable()};
$('batchOffOnly').onchange=()=>{st.batchOffSet=null;renderBatchTable()};
$('batchDry').onchange=()=>renderBatchTable();
// Les lignes hors du lot en cours montrent la dernière décision de l'historique : la liste est relue
// au départ et à la fin d'un lot, sinon un lot précédent (une simulation) disparaîtrait du tableau.
async function batchPoll(){clearTimeout(st.batchTimer);try{const j=await api('GET','/ui/batch/status');
  // Fin du lot : on ouvre son résultat.
  if(st.batchWasRunning&&!j.running){st.batchTab=j.dry_run?BS.sim:BS.pub;st.batchOffSet=null;if(st.batchPick)await batchPickLoad().catch(()=>{})}st.batchWasRunning=!!j.running;
  renderBatch(j);if(j.running)st.batchTimer=setTimeout(batchPoll,2000)}catch(e){}}
$('batchStart').onclick=()=>run($('batchStart'),async()=>{const dry=$('batchDry').checked,include=batchIncluded();
  if(include&&!include.length)return toast('Rien de coché ici : rien à traiter.','warn');
  if(!dry&&!confirm(`Publier pour de vrai ${include?include.length+' release(s)':'tout le dossier'} ? Seul ce qui est sûr part ; le reste reste « à revoir ».`))return;
  await api('POST','/ui/batch/start',{path:$('batchPath').value.trim(),dry_run:dry,max:+$('batchMax').value||0,limit:+$('batchLimit').value||0,max_size:Math.round((+$('batchMaxSize').value||0)*1024**3),only_video:st.batchKind==='video',music:st.batchKind==='music',music_source:$('batchSrc').value,category_film:$('batchCatFilm').value,category_tv:$('batchCatTV').value,include});
  toast(dry?'Simulation lancée':'Lot lancé','ok');st.batchTab=BS.wait;st.batchOffSet=null;await batchPoll();await batchPickLoad().catch(()=>{})});
// Contenu : vidéos seulement, musique (un album = une release) ou tout. Les catégories film/série ne servent pas aux albums.
function batchMode(){const m=st.batchKind==='music';$('batchSrcWrap').hidden=!m;$('batchCatFilm').parentElement.hidden=m;$('batchCatTV').parentElement.hidden=m;
  $('batchKind').querySelectorAll('button').forEach(b=>{const on=b.dataset.k===st.batchKind;b.classList.toggle('on',on);b.setAttribute('aria-checked',on)})}
$('batchKind').querySelectorAll('button').forEach(b=>b.onclick=()=>{const was=st.batchKind==='music';st.batchKind=b.dataset.k;batchMode();if(st.batchPick&&was!==(st.batchKind==='music'))batchPickLoad().catch(e=>toast(e.message,'err'))});
// Cases à cocher : une par entrée du dossier (par album en musique), toutes cochées au départ, sauf celles déjà sur Draupnirr d'après l'historique.
const batchPickKey=()=>$('batchPath').value.trim()+'|'+(st.batchKind==='music'?1:0);
// null = pas de liste pour ce dossier : tout y passe. Sinon les cochées affichées (onglet, recherche), pour ne publier que ce qui a été montré.
function batchIncluded(){const p=st.batchPick;if(!p||p.key!==batchPickKey())return null;return st.batchSel}
async function batchPickLoad(){const path=$('batchPath').value.trim();if(!path)return toast('Choisis un dossier','warn');const key=batchPickKey();
  $('batchPickCount').textContent='Lecture du dossier…';
  const r=await api('GET','/ui/batch/list?path='+encodeURIComponent(path)+(st.batchKind==='music'?'&music=1':''));if(key!==batchPickKey())return;
  const prev=st.batchPick&&st.batchPick.key===key?st.batchPick:null;const entries=r.entries||[];
  st.batchPick={key,path,entries,on:new Set(entries.filter(e=>e.last?.status!==BS.pub&&(prev&&prev.entries.some(x=>x.path===e.path)?prev.on.has(e.path):!bDone(e.last)&&!e.choice?.ignored)).map(e=>e.path))};st.batchOffSet=null;renderBatchTable()}
$('batchPickLoad').onclick=()=>run($('batchPickLoad'),()=>batchPickLoad().catch(e=>{$('batchPickCount').textContent=e.message;toast(e.message,'err')}));
$('batchPath').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('batchPickLoad').click()}});
$('batchPath').addEventListener('change',()=>{if(st.batchPick&&st.batchPick.key!==batchPickKey())$('batchPickLoad').click()});
// Sélecteur de dossier : on navigue dans les dossiers de la source (sans toucher au dossier de l'onglet Upload).
async function ddBrowse(p){const seq=st.ddSeq=(st.ddSeq||0)+1;$('ddList').innerHTML='<div class="empty"><span>Lecture du dossier…</span></div>';
  try{const r=await api('GET','/ui/browse?peek=1&dirs=1&path='+encodeURIComponent(p));if(seq!==st.ddSeq)return;$('ddPath').value=r.path;$('ddPath').dataset.parent=r.parent||'';$('ddUp').disabled=!r.parent||r.parent===r.path;
    const ds=(r.entries||[]).filter(e=>e.is_dir).sort((a,b)=>a.name.localeCompare(b.name)),nf=(r.entries||[]).length-ds.length;
    $('ddCount').textContent=`${ds.length} dossier(s)${nf?`, ${nf} fichier(s)`:''}`;
    $('ddList').innerHTML=ds.map(e=>`<div class="e dir" data-p="${esc(e.path)}">${ico('folder')}<span class="n" title="${esc(e.path)}">${esc(e.name)}</span><span class="sz">${e.size?human(e.size):''}</span>${ico('chevron-right')}</div>`).join('')||'<div class="empty"><span>Aucun sous-dossier.</span></div>';
    $('ddList').querySelectorAll('.e').forEach(d=>d.onclick=()=>ddBrowse(d.dataset.p))}
  catch(e){if(seq===st.ddSeq)$('ddList').innerHTML=msg('err',esc(e.message))}}
$('batchBrowse').onclick=()=>{$('dirDlg').showModal();ddBrowse($('batchPath').value.trim())};
$('ddUp').onclick=()=>ddBrowse($('ddPath').dataset.parent||'');
$('ddPath').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();ddBrowse($('ddPath').value.trim())}});
$('ddClose').onclick=$('ddCancel').onclick=()=>$('dirDlg').close();
$('ddOk').onclick=()=>{$('batchPath').value=$('ddPath').value;$('dirDlg').close();$('batchPickLoad').click()};
$('batchStop').onclick=()=>api('POST','/ui/batch/stop').then(()=>toast('Arrêt demandé après la release en cours','info'));

// ---- Loupe : détail d'une ligne, œuvre ou édition choisie à la main ----
// Un choix fait passer la ligne « revu » : le lot suivant prend cette œuvre au lieu de chercher.
st.bd=null;
const bdGuess=n=>n.replace(/\.[a-z0-9]{2,4}$/i,'').split(/[ ._(\[-](?:19|20)\d\d\b|[ ._]S\d\d|[ ._](?:2160|1080|720|576|480)p|\[/i)[0].replace(/[._]+/g,' ').trim();
function batchOpen(path){const r=batchRows().rows.find(x=>x.path===path);if(!r)return;
  const music=st.batchKind==='music',[artist,album]=r.name.includes(' - ')?r.name.split(' - ',2):['',r.name],ch=r.choice?.ignored?null:r.choice;
  // Le choix déjà fait revient tel quel : œuvre, saison et facettes retouchées.
  const pick=ch?(ch.musicbrainz_id?{title:ch.title,year:ch.year,sub:ch.artist||'',img:'',choice:ch}:{title:ch.title,year:ch.year,sub:ch.tmdb_type===TK.tv?'Série':'Film',img:ch.poster_url,choice:ch}):null;
  st.bd={r,music,pick,kind:ch?.tmdb_type||(r.episode?TK.tv:TK.movie),q:ch&&!music?ch.title:bdGuess(r.name),artist:ch?.artist||artist.trim(),album:ch&&music?ch.title:bdGuess(album),results:[],
    fiche:null,facets:{...(ch?.facets||{})},episode:ch?.episode||'',cat:ch?.category||''};
  $('bdName').textContent=r.name;$('bdSub').textContent=`${human(r.size)} · ${r.path}`;
  $('bdBadge').innerHTML=r.status?`<span class="bdg ${bBadge(r.status)}">${esc(r.status)}</span>`:'';
  batchDlg();bdShow();if(batchCanChoose(r)){batchSearch();if(!music&&!r.choice?.ignored)batchPrepare()}}
// Focus sur le corps : Entrée valide au lieu d'activer le bouton Fermer.
function bdShow(){if(!$('batchDlg').open)$('batchDlg').showModal();$('bdBody').scrollTop=0;$('bdBody').focus({preventScroll:true})}
// ---- Revue en série : la loupe parcourt une file (les lignes affichées), sans se refermer ----
// done : lignes réglées pendant la revue (validées, ignorées, ou rattachées à la même œuvre).
st.bq=null;st.bdOpen={ft:false,mi:false};
function bqOpen(paths,start){st.bq={paths:[...paths],pos:Math.max(0,paths.indexOf(start)),done:new Set()};batchOpen(st.bq.paths[st.bq.pos])}
function bqMove(dir){const q=st.bq,p=q.pos+dir;if(p<0||p>=q.paths.length)return;q.pos=p;batchOpen(q.paths[p])}
function bqNext(){const q=st.bq;let p=q.pos+1;while(p<q.paths.length&&q.done.has(q.paths[p]))p++;q.pos=p;
  if(p<q.paths.length)return batchOpen(q.paths[p]);
  const n=q.paths.length,k=q.paths.filter(x=>q.done.has(x)).length;st.bd=null;
  $('bdName').textContent='File terminée';$('bdSub').textContent='';$('bdBadge').innerHTML='';$('bdQueue').hidden=true;
  $('bdBody').innerHTML=`<div class="endq">${ico('check')}<b>${k} release(s) traitée(s) sur ${n}</b><span class="mut">Elles sont dans Revu ou Ignoré ; le prochain lot s'en servira.</span></div>`;
  $('bdFoot').innerHTML='<span class="grow"></span><button type="button" class="primary" id="bdCancel">Fermer</button>';$('bdCancel').onclick=()=>$('batchDlg').close();bdShow()}
function bqBar(){const q=st.bq,b=$('bdQueue');b.hidden=!q||q.paths.length<2||!!st.bd?.group;if(b.hidden)return;const n=q.paths.length,k=q.paths.filter(x=>q.done.has(x)).length;
  b.innerHTML=`<button type="button" class="ghost" id="bdPrev" aria-label="Précédente" ${q.pos?'':'disabled'}>${ico('chevron-left')}</button><span class="pos">${q.pos+1} / ${n}</span><button type="button" class="ghost" id="bdNext" aria-label="Suivante" ${q.pos<n-1?'':'disabled'}>${ico('chevron-right')}</button><div class="bar"><div style="width:${Math.round(k/n*100)}%"></div></div>`;
  $('bdPrev').onclick=()=>bqMove(-1);$('bdNext').onclick=()=>bqMove(1)}
document.addEventListener('keydown',e=>{const q=st.bq,d=st.bd;if(!$('batchDlg').open||!q||!d||q.paths.length<2||e.ctrlKey||e.metaKey||e.altKey||e.target.matches('input:not([type=checkbox]),select,textarea'))return;
  if(e.key==='ArrowRight'){e.preventDefault();bqMove(1)}else if(e.key==='ArrowLeft'){e.preventDefault();bqMove(-1)}
  else if(e.key==='Enter'&&!e.target.closest('button,a')&&$('bdOk')&&!$('bdOk').disabled){e.preventDefault();$('bdOk').click()}
  else if((e.key==='i'||e.key==='I')&&$('bdIgnore')){e.preventDefault();$('bdIgnore').click()}});
// Même œuvre pour plusieurs releases : titre lu dans le nom ; chaque release garde sa saison (S02, S04E06).
// La saison vient du serveur (episodeOf, liste du lot) : une seule lecture du nom, la même qu'à la publication.
const bdTitle=n=>bNorm(bdGuess(n));
// Autres lignes du même titre dans l'onglet, sans choix encore : proposées, cochées, à la validation.
const bdSibs=d=>{if(d.music||d.group||!d.pick)return [];const t=bdTitle(d.r.name),tab=st.batchTab;
  return batchRows().rows.filter(x=>x.path!==d.r.path&&!x.choice&&batchCanChoose(x)&&(tab==='*'||(x.status||'')===tab)&&bdTitle(x.name)===t)};
const bdWork=(d,x)=>({...d.pick.choice,episode:bdKindOf(d)===TK.tv?x.episode:'',category:d.cat});
// Titre cherché : le plus fréquent du groupe ; série dès qu'une release porte une saison.
function batchGroupOpen(paths){const rows=batchRows().rows,g=paths.map(p=>rows.find(x=>x.path===p)).filter(Boolean);if(g.length<2)return;const r=g[0];st.bq=null;
  const n={};g.forEach(x=>{const t=bdGuess(x.name);n[t]=(n[t]||0)+1});const t=Object.keys(n).sort((a,b)=>n[b]-n[a])[0];
  // Précochées : les lignes de ce titre ; un autre titre coché par mégarde ne part pas sous la mauvaise œuvre.
  st.bd={r,group:g,on:new Set(g.filter(x=>bdGuess(x.name)===t).map(x=>x.path)),music:false,pick:null,kind:g.some(x=>x.episode)?TK.tv:TK.movie,q:t,results:[],fiche:null,facets:{},episode:'',cat:''};
  $('bdName').textContent=`${t} · ${g.length} releases`;$('bdSub').textContent=g.map(x=>x.episode||'—').join(' · ');$('bdBadge').innerHTML='';
  batchDlg();bdShow();batchSearch()}
const batchCanChoose=r=>!st.batchJob?.running&&[BS.rev,BS.sim,BS.rv,BS.err,BS.ign,''].includes(r.status||'');
// Le type suit l'œuvre choisie (le sélecteur de recherche peut basculer sans rien choisir).
const bdKindOf=d=>d.pick?.choice.tmdb_type||d.kind,bdCat=k=>$(k===TK.tv?'batchCatTV':'batchCatFilm').value,bdCatOf=d=>d.cat||bdCat(bdKindOf(d));
// Sous-catégories proposées : la famille (groupe de l'onglet Upload) de la catégorie du lot pour ce type.
const catFamily=v=>{const g=$('category').querySelector(`option[value="${CSS.escape(v)}"]`)?.parentElement;return g&&g.tagName==='OPTGROUP'?g.innerHTML:$('category').innerHTML};
const bdCatOpts=d=>catFamily(bdCat(bdKindOf(d)));
function batchDlg(){const d=st.bd,r=d.r,g=d.group,edit=!!g||batchCanChoose(r),q=st.bq,nx=!g&&q&&q.pos<q.paths.length-1;let h='';bqBar();
  if(!g){
  if(r.status===BS.rev)h+=msg('warn','Mis à revoir : '+esc(r.detail||''));
  if(r.status===BS.rv)h+=`<div class="msg rv">${ico('st-rv')}<div>${esc(r.detail||'')}. Choix fait à la main : la ligne reste « revu » jusqu'à sa publication.</div></div>`;
  if(r.status===BS.err)h+=msg('err',esc(r.detail||''));
  if(r.status===BS.ign)h+=msg('info',r.choice?.ignored?'Ignorée à la main : les lots la sautent sans la hacher.':'Ignorée : '+esc(r.detail||''));
  if(r.status===BS.pub)h+=msg('ok',`${esc(r.detail||'Publié')}${r.url?` · <a href="${esc(r.url)}" target="_blank" rel="noopener">voir la release sur Draupnirr</a>`:''}`);
  if(r.status===BS.present)h+=msg('info','Déjà sur Draupnirr : '+esc(r.detail||''));
  h+=`<dl class="kv"><dt>Chemin</dt><dd class="mono">${esc(r.path)}</dd><dt>Taille</dt><dd>${human(r.size)}</dd>${r.built&&(!edit||d.music)?`<dt>Nom Draupnirr</dt><dd class="mono">${esc(r.built)}</dd>`:''}${r.tmdb?`<dt>Œuvre</dt><dd>${esc(r.tmdb)}</dd>`:''}${r.edition?`<dt>Édition</dt><dd>${esc(r.edition)}</dd>`:''}</dl>`;
  // Arborescence et MediaInfo, repliées (ouvertes d'une release à l'autre si on les a ouvertes) :
  // chaque dossier se lit à l'ouverture, sans hacher ; le rapport est celui de la préparation.
  if(r.is_dir!==false)h+=`<details class="ftree" id="bdTree" ${st.bdOpen.ft?'open':''}><summary>${ico('folder')}Fichiers et dossiers</summary><div class="ft" data-path="${esc(r.path)}"></div></details>`;
  if(edit&&!d.music)h+=`<details class="ftree" id="bdMi" ${st.bdOpen.mi?'open':''}><summary>${ico('film')}MediaInfo<span class="sz"></span></summary><div class="mib"></div></details>`}
  const sb=bdSibs(d);if(sb.length&&!d.also)d.also=new Set(sb.map(x=>x.path));
  const k=g?d.on.size:1+sb.filter(x=>d.also?.has(x.path)).length;
  if(edit){const p=d.pick;
    h+=`<h2>${d.music?'Édition MusicBrainz':'Œuvre TMDB'}</h2>`;
    if(p)h+=`<div class="cur">${p.img?`<img src="${esc(p.img)}" alt="" onerror="this.style.visibility='hidden'">`:''}<span class="grow"><b>${esc(p.title)}${p.year?` <span class="mut">(${p.year})</span>`:''}</b><br><span class="mut">${esc(p.sub)}${p.choice===d.r.choice?' · choix enregistré':' · nouveau choix'}</span></span></div>`;
    h+=`<form class="srch" id="bdForm">${d.music?`<input id="bdArtist" placeholder="Artiste" value="${esc(d.artist)}"><input id="bdQ" placeholder="Album" value="${esc(d.album)}">`:`<select id="bdKind" aria-label="Type"><option value="movie">Film</option><option value="tv">Série</option></select><input id="bdQ" placeholder="Titre de l'œuvre sur TMDB" value="${esc(d.q)}">`}<button type="submit" id="bdSearch">${ico('search')}Chercher</button></form><div class="tmdb ${d.music?'mb':''}" id="bdRes"></div>`;
    const also=(lab,xs,key,on)=>`<div class="also"><div class="ah">${ico('st-rv')}<span>${lab}</span></div>${xs.map(x=>`<label><input type="checkbox" data-${key}="${esc(x.path)}" ${on.has(x.path)?'checked':''}><span class="n" title="${esc(x.path)}">${esc(x.name)}</span><span class="ep">${esc(x.episode||'—')}</span></label>`).join('')}</div>`;
    if(g)h+=also(`Une seule œuvre pour ces ${g.length} releases ; chacune garde sa saison.${d.on.size<g.length?' Les autres titres sont décochés.':''}`,g,'g',d.on)+`<div class="grid"><label>Catégorie<select id="bdCat">${bdCatOpts(d)}</select></label></div>`;
    else if(sb.length)h+=also('Même titre dans l\'onglet : appliquer aussi à',sb,'s',d.also);
    if(!d.music&&!g)h+=`<h2>Fiche</h2><div id="bdFiche" class="stack"></div>`}
  $('bdBody').innerHTML=h;
  // À gauche : écarter la release (onglet Ignoré, gardé d'un lot à l'autre), revenir sur ce choix, ou passer à la suivante.
  const ign=!g&&r.choice?.ignored,okLab=g?`Valider les ${k}`:k>1?`Valider ces ${k}${nx?' et suivante':''}`:nx?'Valider et suivante':r.status===BS.rv||r.status===BS.sim?'Enregistrer':'Valider : passer en Revu';
  $('bdFoot').innerHTML=(edit?(ign?`<button type="button" id="bdForget">Ne plus ignorer</button>`:`<button type="button" id="bdIgnore">${ico('st-skip')}Ignorer${g?` ces ${k}`:''}</button>`):'')+
    (r.status===BS.rv&&edit&&!g?'<button type="button" id="bdForget">Oublier ce choix</button>':'')+(nx?'<button type="button" class="ghost" id="bdSkip">Passer</button>':'')+'<span class="grow"></span><button type="button" id="bdCancel">Fermer</button>'+
    (edit?`<button type="button" class="primary" id="bdOk" ${d.pick&&k?'':'disabled'}>${ico('check')}${okLab}</button>`:'')+
    (q&&q.paths.length>1&&!g?`<div class="keys"><span><kbd>←</kbd> <kbd>→</kbd> naviguer</span>${edit?'<span><kbd>Entrée</kbd> valider</span><span><kbd>I</kbd> ignorer</span>':''}<span><kbd>Échap</kbd> fermer</span></div>`:'');
  $('bdCancel').onclick=()=>$('batchDlg').close();if($('bdSkip'))$('bdSkip').onclick=()=>bqMove(1);
  const tr=$('bdTree'),mi=$('bdMi');
  if(tr){tr.addEventListener('toggle',()=>{st.bdOpen.ft=tr.open;if(tr.open)ftLoad(tr.querySelector('.ft'))});if(tr.open)ftLoad(tr.querySelector('.ft'))}
  if(mi){mi.addEventListener('toggle',()=>{st.bdOpen.mi=mi.open;miLoad(d)});miLoad(d)}
  $('bdBody').querySelectorAll('[data-s],[data-g]').forEach(c=>c.onchange=()=>{const set=c.dataset.g?d.on:d.also,p=c.dataset.g||c.dataset.s;c.checked?set.add(p):set.delete(p);
    const n=g?d.on.size:1+sb.filter(x=>d.also.has(x.path)).length;$('bdOk').innerHTML=ico('check')+(g?`Valider les ${n}`:n>1?`Valider ces ${n}${nx?' et suivante':''}`:nx?'Valider et suivante':okLab);$('bdOk').disabled=!d.pick||!n;
    if(g&&$('bdIgnore'))$('bdIgnore').innerHTML=ico('st-skip')+`Ignorer ces ${n}`});
  if(edit){if($('bdKind')){$('bdKind').value=d.kind;$('bdKind').onchange=()=>{d.kind=$('bdKind').value;batchSearch()}}
    onSubmit($('bdForm'),()=>run($('bdSearch'),batchSearch));batchResults();batchFiche();
    if(g){$('bdCat').value=bdCatOf(d);if($('bdCat').value!==bdCatOf(d)){d.cat='';$('bdCat').value=bdCatOf(d)}$('bdCat').onchange=()=>{d.cat=$('bdCat').value}}
    const mem=()=>g.filter(x=>d.on.has(x.path));
    $('bdOk').onclick=()=>run($('bdOk'),()=>batchChoose(g?mem().map(x=>({r:x,choice:bdWork(d,x)}))
      :[{r,choice:d.music?d.pick.choice:{...d.pick.choice,episode:bdKindOf(d)===TK.tv?d.episode:'',facets:d.facets,category:d.cat}},...sb.filter(x=>d.also.has(x.path)).map(x=>({r:x,choice:bdWork(d,x)}))]));
    if($('bdForget'))$('bdForget').onclick=()=>run($('bdForget'),()=>batchChoose([{r,choice:null}]));
    if($('bdIgnore'))$('bdIgnore').onclick=()=>run($('bdIgnore'),()=>batchChoose((g?mem():[r]).map(x=>({r:x,choice:{ignored:true}}))))}}
// Aperçu MediaInfo : pistes résumées en puces, puis le rapport complet (JSON de mediainfo, mis en colonnes).
const MI_CH={1:'1.0',2:'2.0',3:'2.1',6:'5.1',7:'6.1',8:'7.1'};
function miHtml(mi){const t=mi?.media?.track||[],ty=x=>x['@type'],v=t.find(x=>ty(x)==='Video'),au=t.filter(x=>ty(x)==='Audio'),tx=t.filter(x=>ty(x)==='Text'),w=+v?.Width||0;
  const res=!v?'':w>=3800?'2160p':w>=1900?'1080p':w>=1260?'720p':v.Height?v.Height+'p':'';
  const chips=[res,v?.Format,v?.BitDepth&&v.BitDepth+' bits',v?.HDR_Format&&'HDR',...au.map(a=>[a.Format,MI_CH[a.Channels]||(a.Channels?a.Channels+' can.':''),(a.Language||'').toUpperCase()].filter(Boolean).join(' · ')),tx.length?tx.length+' sous-titre'+(tx.length>1?'s':''):''].filter(Boolean);
  const rep=t.map(x=>ty(x)+(t.filter(y=>ty(y)===ty(x)).length>1&&x['@typeorder']?' #'+x['@typeorder']:'')+'\n'+Object.entries(x).filter(([k,v])=>!k.startsWith('@')&&typeof v!=='object').map(([k,v])=>k.padEnd(28)+' : '+v).join('\n')).join('\n\n');
  return (chips.length?`<div class="mih">${chips.map(c=>`<span class="chip probed">${esc(c)}</span>`).join('')}</div>`:'')+`<pre>${esc(rep||'Rapport vide.')}</pre>`}
// Section MediaInfo repliée (loupe, Upload) : c porte job, miErr, fiche, et garde le rendu (c.mi).
async function miShow(det,c){if(!det||!det.open||!c)return;const b=det.querySelector('.mib');
  if(c.mi){b.innerHTML=c.mi.html;det.querySelector('summary .sz').textContent=c.mi.name;return}
  if(!c.job){b.innerHTML=c.fiche?.err?msg('err','Préparation : '+esc(c.fiche.err)):'<div class="mut">Lu pendant la préparation…</div>';return}
  if(c.miErr){c.mi={html:msg('warn','MediaInfo : '+esc(c.miErr)),name:''};return miShow(det,c)}
  b.innerHTML='<div class="mut">Lecture…</div>';
  try{let mi=await api('GET','/ui/job/'+c.job+'/mediainfo');if(Array.isArray(mi))mi=mi[0];c.mi={html:miHtml(mi),name:String(mi?.media?.['@ref']||'').split(/[\\/]/).pop()}}
  catch(e){c.mi={html:msg('warn','MediaInfo : '+esc(e.message)),name:''}}
  if(det.isConnected)miShow(det,c)}
const miLoad=d=>{if(st.bd===d)miShow($('bdMi'),d)};
// Fiche de la loupe : même préparation et même analyse que l'onglet Upload (torrent repris du cache
// quand le lot l'a déjà haché), facettes préremplies et modifiables, nom recalculé à chaque retouche.
async function ftLoad(box){if(box.dataset.done)return;box.dataset.done=1;box.innerHTML='<div class="mut">Lecture…</div>';
  try{const r=await api('GET','/ui/browse?peek=1&path='+encodeURIComponent(box.dataset.path));const es=(r.entries||[]).sort((a,b)=>b.is_dir-a.is_dir||a.name.localeCompare(b.name));
    box.innerHTML=es.map(e=>e.is_dir?`<details><summary>${ico('folder')}<span class="t">${esc(e.name)}</span><span class="sz">${e.size?human(e.size):''}</span></summary><div class="ft" data-path="${esc(e.path)}"></div></details>`
      :`<div class="f">${ico('file')}<span class="t">${esc(e.name)}</span><span class="sz">${human(e.size)}</span></div>`).join('')||'<div class="mut">Dossier vide.</div>';
    box.querySelectorAll(':scope>details').forEach(d=>d.addEventListener('toggle',()=>{if(d.open)ftLoad(d.querySelector('.ft'))}))}
  catch(e){delete box.dataset.done;box.innerHTML=msg('err',esc(e.message))}}
async function batchPrepare(){const d=st.bd;d.fiche={step:'hachage',line:''};batchFiche();
  try{// Mêmes gardes que le lot (batchOne) : rien n'est haché au-delà du plafond, ni dans un dossier sans fichier à la racine.
    const cap=Math.round((+$('batchMaxSize').value||0)*1024**3);
    if(cap&&d.r.size>cap)throw new Error(`au-delà du plafond (${human(d.r.size)} > ${human(cap)}) : pas haché`);
    if(d.r.is_dir!==false){const l=await api('GET','/ui/browse?peek=1&dirs=1&path='+encodeURIComponent(d.r.path));if(st.bd!==d)return;
      if(!(l.entries||[]).some(e=>!e.is_dir))throw new Error('aucun fichier à la racine : un dossier de releases ? Coche-les une par une (un disque complet se publie à la main)')}
    const {job}=await api('POST','/ui/prepare',{path:d.r.path,category:bdCatOf(d)});
    const j=await waitJob(job,j=>{if(st.bd!==d)return;d.fiche={step:j.step,line:jobLine(j)};batchFiche()},()=>st.bd===d);
    if(st.bd!==d||!j)return;if(j.error)throw new Error(j.error);
    d.job=job;d.miErr=j.mediainfo_error||'';d.episode||=j.episode||'';d.fiche={an:j.analysis};miLoad(d);
    if(d.pick||Object.keys(d.facets).length||d.episode)return batchReanalyze();batchFiche()}
  catch(e){if(st.bd===d){d.fiche={err:e.message};batchFiche();miLoad(d)}}}
async function batchReanalyze(){const d=st.bd;if(!d?.job)return;const c=d.pick?.choice;$('bdFiche')?.classList.add('loading');
  try{const a=await api('POST','/ui/analyze',{job:d.job,category:bdCatOf(d),facets:d.facets,episode:bdKindOf(d)===TK.tv?d.episode:'',
      ...(c?{work_title:c.title,year:c.year||'',tmdb_id:c.tmdb_id,tmdb_type:c.tmdb_type}:{})});
    if(st.bd===d){d.fiche={an:a};batchFiche()}}
  catch(e){toast('Analyse : '+e.message,'err')}finally{$('bdFiche')?.classList.remove('loading')}}
function batchFiche(){const d=st.bd,box=$('bdFiche'),f=d?.fiche;if(!box||!f)return;
  if(f.err){box.innerHTML=msg('err','Préparation : '+esc(f.err));return}
  if(!f.an){box.innerHTML=`<div class="mut">${esc(f.step==='hachage'?'Hachage':f.step==='mediainfo'?'MediaInfo':'Analyse')}… ${esc(f.line||'')}</div>`;return}
  const a=f.an,n=a.nomenclature;
  if(!n){box.innerHTML=msg('warn','Pas de nomenclature automatique pour cette catégorie.');return}
  const keys=[...new Set(['source','edition','group','languages',...Object.keys(n.facets||{})])];
  box.innerHTML=`<div class="built"><div class="eyebrow">Nom qui sera publié</div><div class="mono big">${esc(n.built_name||a.name)}</div><div class="chips">${facetChips(n)}</div>
    ${n.missing?.length?`<div class="warn-text">Manque : ${esc(n.missing.join(', '))}</div>`:''}</div>
    ${d.miErr?msg('warn','MediaInfo : '+esc(d.miErr)):''}
    <div class="grid" id="bdFacets"><label>Catégorie<select id="bdCat">${bdCatOpts(d)}</select></label>${bdKindOf(d)===TK.tv?`<label>Épisode (séries)<input id="bdEp" placeholder="S01E03" value="${esc(d.episode)}"></label>`:''}${facetGrid(n,keys,d.facets)}</div>`;
  box.querySelectorAll('[data-f]').forEach(el=>el.onchange=()=>{d.facets[el.dataset.f]=el.value;batchReanalyze()});
  $('bdCat').value=bdCatOf(d);if($('bdCat').value!==bdCatOf(d)){d.cat='';$('bdCat').value=bdCatOf(d)}$('bdCat').onchange=()=>{d.cat=$('bdCat').value;batchReanalyze()};
  if($('bdEp'))$('bdEp').onchange=()=>{d.episode=$('bdEp').value.trim().toUpperCase();batchReanalyze()}}
async function batchSearch(){const d=st.bd;if(!$('bdQ'))return;d.q=$('bdQ').value;if(d.music){d.artist=$('bdArtist').value;d.album=$('bdQ').value}
  $('bdRes').innerHTML=`<div class="empty">Recherche sur ${d.music?'MusicBrainz':'TMDB'}…</div>`;
  const r=d.music?await api('GET','/ui/musicbrainz?'+new URLSearchParams({artist:d.artist,album:d.album})).catch(e=>({results:[],error:e.message}))
    :await api('GET','/ui/tmdb?q='+encodeURIComponent(d.q)+'&type='+d.kind).catch(e=>({results:[],error:e.message}));
  d.results=(r.results||[]).slice(0,8);d.error=r.error;batchResults()}
function batchResults(){const d=st.bd,box=$('bdRes');if(!box)return;
  box.innerHTML=d.results.map((x,i)=>d.music?`<button type="button" class="c ${d.pick?.choice.musicbrainz_id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.cover_url||'')}" alt="" loading="lazy" onerror="this.style.visibility='hidden'"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc([x.artist,x.label,x.country,x.media].filter(Boolean).join(' · '))}</div><span class="bdg x">${x.track_count} pistes</span></div></button>`
      :`<button type="button" class="c ${d.pick?.choice.tmdb_id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.poster_url||'')}" alt="" loading="lazy" onerror="this.style.visibility='hidden'"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc(x.overview||'')}</div></div></button>`).join('')
    ||`<div class="empty">${d.error?esc(d.error):'Aucun résultat : essaie un autre titre'+(d.music?'.':' ou l\'autre type.')}</div>`;
  box.querySelectorAll('.c').forEach(b=>b.onclick=()=>{const x=d.results[+b.dataset.i],y=+x.year||0;
    // Film ↔ série : la sous-catégorie choisie ne vaut plus, celle du lot reprend.
    if(!d.music&&d.pick?.choice.tmdb_type&&d.pick.choice.tmdb_type!==d.kind)d.cat='';
    d.pick=d.music?{title:x.title,year:y,sub:[x.artist,x.media].filter(Boolean).join(' · '),img:x.cover_url,choice:{musicbrainz_id:x.id,title:x.title,artist:x.artist||'',year:y}}
      :{title:x.title,year:y,sub:d.kind===TK.tv?'Série':'Film',img:x.poster_url,choice:{tmdb_id:x.id,tmdb_type:d.kind,title:x.title,year:y,overview:x.overview||'',poster_url:x.poster_url||''}};
    const keep={q:$('bdQ').value,a:$('bdArtist')?.value};batchDlg();$('bdQ').value=keep.q;if(keep.a!=null)$('bdArtist').value=keep.a;if(!d.music&&!d.group)d.fiche?batchReanalyze():batchPrepare()})}
// items : [{r, choice}] — la release ouverte, plus les lignes du même titre ou du groupe.
async function batchChoose(items){const d=st.bd,q=st.bq,c=items[0].choice,n=items.length,wasIgn=items[0].r.choice?.ignored;
  for(const it of items)await api('POST','/ui/batch/choice',{path:it.r.path,name:it.r.name,size:it.r.size,choice:it.choice});
  // Ignorée : décochée ; de retour : recochée.
  if(st.batchPick)items.forEach(it=>{if(it.choice?.ignored)st.batchPick.on.delete(it.r.path);else if(it.r.choice?.ignored)st.batchPick.on.add(it.r.path)});
  toast(c?.ignored?(n>1?`${n} releases ignorées : elles passent dans Ignoré`:'Release ignorée : elle passe dans Ignoré'):c?`« ${c.title} » retenue${n>1?` pour ${n} releases : elles passent`:' : la ligne passe'} dans Revu`:wasIgn?'La release n\'est plus ignorée':'Choix oublié : la ligne repasse à revoir','ok');
  await batchPickLoad().catch(()=>{});batchPoll();
  if(st.bd!==d)return;
  // File : un choix fait passer à la suivante encore à régler ; un oubli rouvre la même.
  if(q&&q.paths.length>1&&!d.group){if(c){items.forEach(it=>q.done.add(it.r.path));bqNext()}else batchOpen(q.paths[q.pos])}else $('batchDlg').close()}
$('bdClose').onclick=()=>$('batchDlg').close();
$('batchDlg').addEventListener('close',()=>{st.bd=null;st.bq=null});
