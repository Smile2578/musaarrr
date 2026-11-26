# Musaarr

Demander un album hi-fi depuis l'iPhone ou l'iPad, en un geste, et le suivre jusqu'à ce qu'il soit dans Plex.

Musaarr cherche les albums par Lidarr (pochettes comprises), ajoute **uniquement** l'album demandé avec le profil de qualité choisi, lance sa recherche, puis affiche son état : Demandé, Recherche en cours, En attente d'une source, Téléchargement N %, Import en cours, À vérifier, Échec du téléchargement, Importé, Source choisie, Dans Plex. La recherche montre d'abord les artistes que la requête nomme (« Middle of Nowhere – Kacey Musgraves » mène à Kacey Musgraves), et chaque album ouvre sa page. Sur la page d'un artiste, chaque album montre la meilleure sortie que ses indexeurs ont (par le Prowlarr de l'utilisateur), et sa page liste ces sorties pour en télécharger une soi-même. Les albums peuvent se ranger par écoutes sur ListenBrainz, et l'accueil propose les incontournables des artistes déjà dans Plex.

Depuis la v3, la Bibliothèque est une pièce à décorer (étagères de pochettes, bac à fouiller), l'accueil de Rechercher est un disquaire (depuis la v4, des racks de bacs par genre qu'on feuillette, avec les pochettes des pressages japonais), et un disque en 3D (three.js) ouvre l'appli à la première visite et tourne derrière la recherche.

- Un binaire Go sans dépendance, interface web embarquée (français, mobile d'abord, thèmes clair et sombre, installable sur l'écran d'accueil).
- Une connexion par compte : la première visite crée le compte administrateur, les suivantes demandent son nom et son mot de passe. Derrière un proxy qui identifie déjà chacun (Tailscale serve, Authelia, authentik), son en-tête suffit, s'il vient d'une adresse listée (voir « Connexion »).
- Les clés de Lidarr, de Plex et de Prowlarr restent côté serveur ; le navigateur ne voit que des objets réduits.
- Fichiers d'état, dans `/data` : `requests.json` (qui a demandé quoi, quand), `room.json` (la décoration de la pièce), `stock.json.gz` (le stock de départ du disquaire, téléchargé), `shop.json` (le stock du disquaire, reconstruit chaque jour), `obi.json` (les pochettes japonaises trouvées) et `covers/` (les pochettes du disquaire, gardées dans 1 Go, avec `facts.jsonl`, leurs couleurs). Tout le reste est relu dans Lidarr et Plex.

Musaarr n'héberge, n'indexe ni ne distribue aucun contenu. Il pilote le Lidarr et le Plex de l'utilisateur et, si on le branche, son Prowlarr, sans aucun indexeur fourni ni conseillé. Chacun répond de ce qu'il télécharge, selon le droit de son pays. Le projet n'est affilié ni à Plex Inc., ni à MetaBrainz, ni aux projets Lidarr et Prowlarr.


## Comment une demande est faite

1. `GET /api/v1/album/lookup?term=lidarr:<MBID>` pour relire l'album.
2. Album absent de Lidarr : `POST /api/v1/album` avec l'artiste en profil de métadonnées « None », `monitorNewItems: none` et `albumsToMonitor: [<MBID>]`. Sans cette liste, Lidarr 3.1.6 désactive le suivi de l'album demandé après le premier scan d'un nouvel artiste. Album déjà présent : `PUT /api/v1/album/monitor`.
   Lidarr 3.1.6 crée pourtant le nouvel artiste non suivi (vu sur une vraie instance), ce qui sort l'album de Wanted, de la veille RSS et des mises à niveau : Musaarr le remet suivi avec `PUT /api/v1/artist/editor`. Ses autres albums restent non suivis.
3. Dès que l'album est suivi et a au moins une édition, une seule recherche : la recherche interactive de Lidarr (`GET /api/v1/release?albumId=`), qui envoie la même requête aux indexeurs qu'un `AlbumSearch` mais montre toutes les sources et pourquoi Lidarr les refuse. Musaarr prend la source que Lidarr choisit, sauf qu'entre ses choix le 24 bits à la plus haute fréquence passe d'abord, jusqu'à 96 kHz (96, puis 48 kHz, puis une fréquence que le nom ne donne pas ; un 192 compte comme un 96 : il ne s'entend pas et pèse deux fois plus) : Lidarr ne lit pas les kHz. S'il n'en choisit aucune, Musaarr prend la meilleure source sans perte (24 bits d'abord, puis la plus haute fréquence jusqu'à 96 kHz) refusée seulement parce que Lidarr ne sait pas lire son nom, et seulement si ce nom se lit « artiste ou alias, [année], album » (`POST /api/v1/release` avec `albumId` et `artistId`).
   Lidarr lit les noms à partir du nom MusicBrainz actuel de l'artiste : depuis que Kanye West s'appelle « Ye », « Kanye West - 2005 - Late Registration [FLAC] » est un « Wrong album » et « Kanye.West.Yeezus.2013.FLAC » est illisible. Les alias viennent du serveur de métadonnées (`artistaliases`) ; ils servent aussi à retrouver l'album dans Plex, qui le range sous le nom de ses tags.
4. Quand une source au nom illisible est téléchargée, Lidarr s'arrête sur « Unable to parse download ». Musaarr retrouve le téléchargement par l'historique (`downloadId`), puis lance l'import manuel (`ManualImport`) seulement si chaque fichier audio correspond à une piste distincte de l'album, sans rejet. Sinon, la demande passe « À vérifier » et l'import reste à faire dans Lidarr.

Musaarr ne relance jamais une recherche de lui-même : « Relancer la recherche » en refait une.

« Retirer » (deux touches : « Retirer » puis « Confirmer le retrait » dans les 5 s) enlève une demande faite par erreur : Lidarr arrête de suivre l'album (`PUT /api/v1/album/monitor`, `monitored: false`), et une recherche de Musaarr en cours n'envoie plus rien. Le bouton n'apparaît pas tant qu'une source est en route (téléchargement, import, source choisie, ou sortie retenue par Lidarr) : retirer la demande ne l'arrêterait pas. Une demande déjà dans Plex quitte seulement la liste, sans toucher à Lidarr, qui peut encore mettre l'album à niveau.

Plex compte quelle que soit la source : un album que l'utilisateur a téléchargé lui-même (par Prowlarr, puis rangé par un autre outil) passe « Dans Plex » dès que Plex l'a, sous le nom de l'artiste ou un de ses alias. Si Lidarr n'a pas les fichiers, Musaarr arrête alors de suivre l'album dans Lidarr (`PUT /api/v1/album/monitor`, `monitored: false`), qui sinon en téléchargerait une deuxième copie. Seul un import de Lidarr à moitié fait est attendu avant de regarder Plex. Une demande arrivée dans Plex quitte « En route » : la bibliothèque la montre.

## Bibliothèque

L'onglet « Bibliothèque » montre d'abord « En route », les demandes pas encore dans Plex, puis **tous les albums de Plex** : recherche (titre, artiste ou genre), tri (derniers ajoutés, derniers écoutés, artiste, année, titre), filtres (décennie, genre, jamais écoutés) et une vue par artiste. Le tri et la vue sont gardés sur l'appareil ; la dernière bibliothèque reçue aussi, pour s'afficher tout de suite et rester lisible hors ligne.

- `GET /api/library` : les albums que Plex a donnés à son dernier rechargement (toutes les 10 min, toutes les minutes après un import), encodés une fois, gzip et ETag (le navigateur revalide, Plex n'est pas appelé). Plex est lu avec `includeGuids=1` pour son identifiant MusicBrainz.
- `GET /api/library/{clé}/cover` : la pochette en vignette carrée de 400 px, demandée au transcodeur photo de Plex (l'image d'origine s'il échoue). Le jeton Plex reste dans l'en-tête de la requête de Musaarr ; seules les images JPEG, PNG, WebP et GIF passent ; le navigateur la garde un jour.
- `GET /api/library/{clé}/album` : l'album MusicBrainz derrière un album de Plex, pour ouvrir sa page : l'identifiant `mbid://` de Plex s'il est connu du serveur de métadonnées, sinon la recherche de Lidarr sur « artiste titre » (même artiste et même titre d'abord, puis même titre sous un autre nom, car Plex range Ye sous Kanye West). Sans correspondance, la carte le dit et propose « Ouvrir dans Plex ». La réponse est gardée 10 min.

### La pièce

Au-dessus de la grille, la Bibliothèque est une pièce : des étagères de pochettes posées sur des planches (« Sur la platine », « Ma vitrine », « Les plus écoutés », « Écoutés dernièrement », « Arrivés récemment », « Jamais écoutés », « À redécouvrir », puis une par type que Plex donne : albums, singles et EP, live, compilations, bandes originales, remix), chacune avec un objet et « Fouiller », qui ouvre son bac : un bac de disques où l'on feuillette du doigt, à la molette ou aux flèches. « Décorer » choisit le bois, le mur, l'objet de chaque étagère, les étagères montrées et leur ordre ; « Mettre en vitrine », sur la page d'un album, le pose dans la vitrine (24 disques au plus).

- `GET /api/room` : la décoration, gardée dans `room.json` pour que l'iPhone, l'iPad et l'ordinateur montrent la même pièce.
- `PUT /api/room` : l'enregistre (16 Kio au plus ; ce que cette version ne connaît pas est écarté, une pièce abîmée ne casse jamais la page).

Un artiste déjà présent dans Lidarr n'est jamais modifié ; si son profil n'est pas celui choisi ou s'il n'est pas suivi, la demande l'indique.

## Sources par Prowlarr

Ouvrir un artiste lance **une** recherche sur tous les indexeurs musique de Prowlarr, sous son nom MusicBrainz, par `GET /api/artists/{mbid}/sources` côté Musaarr et `GET /api/v1/search` côté Prowlarr (catégories 3000, 3010 et 3040 ; les livres audio sont écartés). Les albums s'affichent sans l'attendre ; chaque carte garde une ligne pour sa qualité, remplie quand les trackers répondent (« FLAC 24 bits · 3 sorties », ou « Aucune source »). Une ligne sous l'en-tête dit où en est la recherche, ou que les trackers ne répondent pas, avec « Réessayer ».

Chaque sortie est rangée sous **au plus un** album : son nom doit porter le nom de l'artiste (ou un de ses autres noms) puis le titre de l'album, mots entiers et d'affilée, avec au plus une année ou des mots de format entre les deux (« Kanye West - 2005 - Late Registration »). Si plusieurs albums conviennent, le titre le plus long gagne ; un titre avec une mention finale (« Sable, fable (Deluxe) ») est aussi essayé sans elle. « Bon Iver - For Emma » ne va donc pas sous « Bon Iver, Bon Iver », ni « Weezer - Hurley » sous « Weezer ». Les discographies et ce qui ne va sous aucun album sont dans « Autres sorties sur tes trackers », en bas de la page ; les sorties d'autres artistes sont écartées. Quand rien n'est rangé sous un album (un artiste renommé : les sorties de Ye portent « Kanye West »), la page propose ses autres noms : un geste, une recherche.

Toucher un album ouvre sa page : pochette, « Demander », puis ses sorties. Sous « Demander », une ligne dit ce que le bouton prend : la meilleure version sans perte que Lidarr trouve sur ses indexeurs (avec un profil hi-fi : 24 bits d'abord, à la plus haute fréquence jusqu'à 96 kHz, le 16 bits sinon, jamais de perte, de vinyle ni de MQA), puis le passage au 24 bits si une telle sortie paraît plus tard. La recherche part avec l'album (`GET /api/sources?album=`) : Musaarr range d'abord les sorties de cet album, puis les autres disques de l'artiste (« Bitches Brew Live », « The Complete Bitches Brew Sessions »), puis le reste. Un nom n'est celui de l'album que si son titre s'arrête là : une parenthèse, une virgule, un tiret, une année ou un mot de format, pas « Bitches Brew Live ». Dans chaque groupe, de la meilleure à la moins bonne pour l'utilisateur : 24 bits, 16 bits (en pistes, ou en image + cue à découper), vinyle (« [LP] », « 2LP », « (2 LP) », « [2xLP] ») ou MQA, ISO ou DSD (que peu d'outils de rangement lisent), format inconnu, puis avec perte ; à niveau égal la plus haute fréquence d'abord (24/192, 24/96, 24/48, puis une fréquence que le nom ne donne pas), les pistes avant l'image + cue, puis les plus partagées, et celles sans source en dernier, grisées. Chaque sortie montre ce que son nom dit de sa qualité (format, bits, kHz, pistes ou image + cue, vinyle, MQA, discographie). Certains indexeurs marquent le 24 bits ou le multicanal par une étiquette à eux dans le titre : les réglages `HIRES_TAGS` et `MULTICHANNEL_TAGS` les font lire. Ouvrir la page lance aussitôt une recherche précise « Artiste Album » : elle trouve plus que celle de l'artiste, qui ne reçoit que 100 résultats par tracker. Quand aucune sortie ne porte tous les mots cherchés, la page propose les autres noms de l'artiste (« Chercher sous Kanye West »). La page d'un album ne lance pas la recherche de l'artiste ; sa page à lui le fait en s'ouvrant.

Pour ce que le catalogue ne connaît pas (bootlegs, discographies, compilations de tracker), « Chercher « … » sur tes trackers », sous les résultats d'une recherche, ouvre la recherche libre : celles dont le nom contient tous les mots cherchés passent devant les réponses approximatives des trackers, séparées par un intertitre. Aucune recherche ne part pendant la frappe.

La page d'un album montre aussi ses **pistes** et ses **éditions** (`GET /api/albums/{mbid}`, qui lit `/album/{mbid}` du serveur de métadonnées et le garde 10 min). Les sorties MusicBrainz qui ont la même liste de pistes (mêmes titres, même ordre) sont une seule édition : « Japon · 2006 · SHM-CD · 22 pistes », avec son nombre de pressages ; la plus pressée vient en premier, et les titres qu'une autre édition a en plus sont marqués « en plus ». Les promos et bootlegs sont écartés s'il reste une sortie officielle. Quand Plex a l'album, la page montre ta version : ses pistes, sa qualité (lue sur la première piste) et l'édition qu'elle reproduit. Quand une sortie de cet album fait nettement mieux (sans perte contre avec perte, plus de bits, ou 88,2 kHz et plus contre une fréquence de CD), une ligne la propose sous ta version, « Mieux que ta version : FLAC · 24 bits · 96 kHz », avec « Remplacer ta version » : la télécharger suffit quand un outil de rangement garde la meilleure copie dans la bibliothèque, et Plex la montre alors à la place de l'ancienne.

Chaque sortie de l'album dit l'édition que son nom désigne, nommée d'après le pressage qu'il vise (« Édition probable : Japon · 2013 · 2 CD · 7 pistes ») : l'année d'un pressage (une année après celle de l'album), le support (CD, vinyle, SACD, numérique ; une sortie en 24 bits sans support nommé est un téléchargement), le nombre de disques, le pays, le label (MFSL compris) et la mention de l'édition sont comparés à chaque pressage, et un mot que toutes les éditions portent ne compte pas. Une bande, une cassette ou un DVD-Audio ne sont aucune des éditions ; sans indice, la sortie est « à confirmer ». Une sortie n'est rangée avec l'album, ou avec les autres disques de l'artiste, que si le nom de l'artiste l'ouvre, après les étiquettes de genre et de source : « The Bitches Brew Band - The Miles Davis Bitches Brew Album » est l'hommage d'un autre. Le pays d'une édition est écrit en français, et une date « 0001-01-01 » du serveur de métadonnées compte comme inconnue. Un bouton par édition trouvée ne montre que ses sorties. La ligne « D'après le nom : édition japonaise, SHM-CD, pressage 2012 » reste en dessous. « Voir le contenu » lit la liste des fichiers de son .torrent (`POST /api/sources/files`) : Musaarr le télécharge par Prowlarr, toujours sur l'hôte Prowlarr configuré et sans suivre de redirection, compte les pistes audio par disque et les rapproche des éditions par leurs titres, puis par leur nombre. Rien n'est envoyé au client de téléchargement ; seuls les noms et tailles des fichiers vont au navigateur. Prowlarr note cette lecture comme une prise dans son historique : elle ne part que sur ce bouton. Un lien magnet, une source Usenet ou un tracker qui garde son .torrent donnent un message clair ; une image + cue est signalée sans compte de pistes. L'édition lue dans les fichiers remplace celle devinée d'après le nom.

« Télécharger » envoie la sortie au client de téléchargement de Prowlarr (`POST /api/v1/search`). Le navigateur ne voit jamais le lien du tracker ni la clé : il reçoit un jeton valable 30 min, utilisable une seule fois.

Depuis une demande en attente, en échec ou à vérifier après un choix, « Choisir une source » ouvre la page de l'album. Une sortie téléchargée de là est rattachée à la demande : elle passe « Source choisie », Musaarr arrête de suivre l'album dans Lidarr pour éviter un doublon, puis la demande passe « Dans Plex » dès que Plex a l'album. Après 24 h sans Plex, elle passe « À vérifier » avec « Pas encore dans Plex : regarde qBittorrent. ». « Relancer la recherche » remet alors l'album suivi dans Lidarr et relance sa recherche.

Sans `PROWLARR_API_KEY`, la page d'un artiste reste sans qualité ni ligne des trackers, et la recherche libre répond « La recherche de sources n'est pas configurée ».

## Écoutes et incontournables

La page d'un artiste demande à ListenBrainz (`GET /1/popularity/top-release-groups-for-artist/{mbid}`, données ouvertes ; depuis novembre 2026 il faut le jeton gratuit d'un compte, `LISTENBRAINZ_TOKEN`) combien chaque album est écouté : mêmes identifiants que Lidarr, aucun rapprochement par titre. Les albums se rangent du plus écouté au moins écouté, ou par date (le choix est gardé). La page n'attend pas ListenBrainz plus de 4 s : sans réponse, elle s'affiche sans écoutes. Sans jeton, il n'y a ni écoutes ni incontournables, et le journal le dit au démarrage.

L'accueil de Rechercher montre « Les incontournables de tes artistes » : pour chaque artiste présent dans Plex (les 80 qui y ont le plus d'albums), ses deux albums studio les plus écoutés qui manquent dans Plex, ni demandés. L'artiste est retrouvé par la recherche d'artistes de Lidarr, sous son nom ou un de ses autres noms. Le calcul tourne au démarrage puis une fois par jour, en tâche de fond, une seconde entre deux artistes (ListenBrainz accepte 30 appels par 10 s, et Lidarr s'essouffle sous une rafale) ; un album demandé ou arrivé dans Plex disparaît aussitôt de la liste.

## Le disquaire

Sous « Quel artiste ? », la vitrine du disquaire : une photo de la boutique, le nombre de rayons et de disques, et « Entrer dans la boutique » (`#disquaire`). D'abord le rayon « Ce mois-ci », les albums les plus écoutés du mois sur ListenBrainz dans l'ordre du classement (bacs « 1–12 », « 13–24 »…), puis 23 rayons : Jazz, Soul et R&B, Funk et disco, Hip-hop, Rock, Indé et alternatif, Pop, Japon, Électro, Ambient et expérimental, Chanson, Classique, Reggae et dub, Afrique et Antilles, Latin et Brésil, Musiques du monde, Blues, Folk, Country, Metal, Punk et hardcore, Bandes originales, Divers.

**La boutique en 3D** (`web/shop3d/`), en plein écran :
- **La salle :** parquet en chevrons, murs enduits, poutres, arcades entre les rangées d'allées. Au-dessus de chaque allée pend une enseigne peinte par rayon ; un rayon qui tient sur plusieurs allées y ajoute ce que chacune contient (« A–F »). Le long des murs, des cubes en bois sont remplis de disques, sous une rangée de pochettes de face. Au fond, le comptoir avec la caisse, une platine qui tourne et un fauteuil.
- **Les meubles :** un par rayon, en chêne, de part et d'autre de l'allée, avec des bacs de 30 disques au plus (les bacs de 12 du serveur, regroupés). Des deux côtés, le premier bac est côté porte : chaque rayon se lit de A à Z (ou de 1 à 100) en avançant. Chaque bac porte sa tranche de lettres, et un intercalaire en kraft annonce chaque artiste qui a plusieurs disques.
- **Les gestes :**
  - La molette ou le doigt fait avancer dans l'allée ; ← → vont d'un bac à l'autre, et les boutons de rayon y mènent directement.
  - Un clic sur un bac fait se pencher dessus. La molette ou ↑ ↓ feuillettent un disque par cran : les disques vus basculent vers toi et reposent les uns sur les autres. Au bout du bac, la molette s'arrête ; un cran de plus après une pause continue dans le bac suivant. ← →, un glisser de côté ou les flèches du panneau passent au bac voisin, puis au meuble d'en face au bout de l'allée (tu te relèves et te tournes vers lui) ; un clic sur un autre bac s'y penche directement.
  - Un clic sur la pochette de devant (ou Entrée) la sort du bac et ouvre la page de l'album. « Disquaire » y ramène au même bac.
  - Échap revient dans l'allée, puis sort de la boutique. « Au hasard » et « Cacher ceux que j'ai » y sont aussi.
- **Ce que charge un appareil :** la boutique lit d'abord l'index des rayons (leurs bacs, sans les disques), puis les disques d'un rayon quand tu approches de son allée. Seuls les bacs proches demandent leurs pochettes, rendues en s'éloignant. Les bacs lointains montrent leur disque de devant et un bloc. Quatre projecteurs suivent tes pas, et seuls les deux plus proches projettent des ombres (un au téléphone). Les pochettes de face des murs tiennent dans une seule image.
- **Les modèles et textures** viennent de Poly Haven (CC0, voir `web/shop3d/assets/CREDITS.txt`). three.js r187 est le même paquet que la scène de l'accueil (`web/vendor/`).

Sans WebGL 2, ou avec « Parcourir les bacs sans la 3D » (choix gardé sur l'appareil), le disquaire reste en racks sur la page, comme ci-dessous ; un lecteur d'écran y trouve tous les disques.

- **Les bacs :** chaque rayon aligne des bacs en bois, de A à Z, qui défilent sur le côté (flèches, glisser à la souris, au doigt). Un bac tient 12 disques au plus et ne coupe jamais un artiste, sauf s'il en a plus de 12 ; un intercalaire en kraft se dresse devant chaque artiste qui a plusieurs disques dans le bac. Le bac porte sa tranche de lettres (« A–B ») ; quand une lettre remplit plusieurs bacs, ses deux premières lettres les distinguent (« He », « Ho »). Un artiste écrit dans un autre alphabet est rangé à son nom de classement MusicBrainz (高中正義 sous T, pour Takanaka), sinon dans un bac « # » en fin de rayon.
- **Feuilleter :** la molette au-dessus d'un bac (un disque par cran ; une molette qui faisait défiler la page continue de la faire défiler), un glisser vers le bas ou le haut, au doigt depuis le haut des disques, ou les flèches du clavier. Les disques basculés passent derrière le rebord du bac. Un clic sur un onglet d'intercalaire saute à l'artiste, un clic sur un disque de derrière l'amène devant.
- **La pochette :** celle d'un pressage japonais quand le Cover Art Archive en a une (souvent avec son obi), sinon la pochette habituelle, sinon une pochette imprimée avec le titre et l'artiste.
- **Ouvrir un disque :** un clic (ou Entrée) sur le disque de devant ouvre la page de l'album ; « Disquaire », en haut de la page, ramène au bac.
- **« Cacher ceux que j'ai déjà »** retire les albums déjà dans Plex ; le choix est gardé sur l'appareil. **« Au hasard »** amène devant son bac un disque absent de la bibliothèque Plex.

Le disquaire part d'un **stock de départ** d'environ 40 000 albums, refait chaque mois par la tâche GitHub `.github/workflows/stock.yml` à partir du dump ouvert de MusicBrainz, rangés par leurs genres sur Wikidata et classés par ListenBrainz, puis publié comme `stock.json.gz` dans la release `stock` (licences et sources : [docs/donnees.md](docs/donnees.md)). Chaque rayon a sa part du stock et chaque décennie sa part du rayon ; un rayon garde 5 % de nouveautés (les trois derniers mois) et 20 % de pépites (les albums moins connus des artistes du rayon), 20 albums par artiste au plus. Musaarr télécharge ce fichier (`STOCK_URL`) au démarrage puis quand il a plus d'une semaine ; un téléchargement raté garde le précédent et réessaie le lendemain. Le fichier est gardé dans `stock.json.gz`.

Avec le jeton ListenBrainz, chaque installation y ajoute sa **couche personnelle**, chaque jour, après les incontournables : les 100 sorties les plus écoutées du mois (`GET /1/stats/sitewide/release-groups?range=this_month`, ou le mois dernier, `range=month`, tant que ListenBrainz n'a pas calculé le mois en cours) et les albums studio des artistes de la bibliothèque, avec leurs étiquettes, leur date et leur type (`GET /1/metadata/release_group/`, par lots de 50, un appel par seconde). Chacun de ces albums va dans le rayon de son genre principal, lu dans ses étiquettes, sinon dans celles de l'artiste ; le nom de classement d'un artiste écrit dans un autre alphabet est demandé une fois au serveur de métadonnées. Seuls les albums restent (ni singles, ni EP, ni émissions). Un album du mois va aussi dans le rayon de son genre. Si le classement du mois ne répond pas, le stock se construit sans le rayon « Ce mois-ci ». Un autre appel qui échoue garde le stock de la veille ; le stock est gardé dans `shop.json` d'un redémarrage à l'autre. Sans stock de départ (`STOCK_URL=off`, ou avant le premier téléchargement), les 1 000 albums les plus écoutés du site depuis toujours en tiennent lieu.

Les pochettes sont préparées en tâche de fond, un album par seconde, dès le démarrage puis chaque heure pour les albums nouveaux : d'abord la couche personnelle, puis les 2 000 premiers albums du stock de départ ; les autres sont lues quand une page les montre. Chaque pochette est gardée dans `covers/` (un identifiant d'image du Cover Art Archive ne change jamais), et pour la pochette japonaise : les sorties japonaises de l'album (`/album/{mbid}` du serveur de métadonnées, l'édition la plus pressée d'abord), puis la face de l'une des trois premières sur le Cover Art Archive (`/release/{mbid}`). La réponse, absence comprise, est gardée dans `obi.json`.

À son arrivée, chaque pochette est lue une fois : sa couleur dominante (la couleur la plus fréquente, et non la moyenne) et son [ThumbHash](https://evanw.github.io/thumbhash/) (une image floue de 24 octets) sont gardés dans `covers/facts.jsonl`, une ligne par pochette. Les racks impriment la pochette d'attente dans sa couleur, le texte au contraste AA, pour que l'image arrive sur sa propre teinte. Les pochettes tiennent dans 1 Go : à la fin de chaque passage, celles qui n'ont pas été lues depuis le plus longtemps partent (une pochette lue est marquée lue au plus une fois par jour) ; leur couleur et leur ThumbHash restent.

- `GET /api/shop/racks` : les rayons, leurs bacs et leurs disques, chacun marqué s'il est dans Plex (`owned`, d'après l'index Plex du moment) et si sa face japonaise est connue (`jp`), avec la couleur dominante (`color`, `#rrggbb`) et le ThumbHash en base64 (`thumb`) de la pochette montrée, une fois lue. `?hide=1` retire les disques déjà dans Plex. `?summary=1` donne les rayons et leurs bacs sans les disques, `?rack=<id>` un seul rayon avec ses disques (404 pour un rayon inconnu) : la boutique 3D lit l'index puis un rayon à la fois. Sans stock de départ ni jeton ListenBrainz, ou pendant le premier remplissage, une réponse 503 le dit.
- `GET /api/shop` : le stock brut (albums, bacs et date), encodé une fois, gzip et ETag.
- `GET /api/shop/{mbid}/cover` : la pochette en 250 px, relayée depuis le Cover Art Archive la première fois puis servie depuis `covers/` (la CSP ne change pas), gardée une semaine par le navigateur. `?jp=1` : la face du pressage japonais, ou 404. `?size=128` : la même en 128 px (JPEG), pour la boutique 3D, faite une fois et gardée à côté. Les pochettes restent en JPEG, comme le Cover Art Archive les sert : Go n'encode pas le WebP sans bibliothèque extérieure.

## La vitrine 3D

À la première visite, une ouverture de quelques secondes : le disque sort de sa pochette, tourne, la marque s'imprime (une touche ou un clic pour passer ; « Revoir l'ouverture » au bas de l'accueil). Ensuite, le disque tourne derrière « Quel artiste ? », accélère avec une onde bleue pendant une recherche, file du bouton « Demander » ou « Télécharger » vers l'onglet Bibliothèque, et un éclat vert dit qu'un album est arrivé dans Plex. La scène est imprimée en trame de points.

three.js r187 est servi par Musaarr (`web/vendor/`, licence MIT, voir [web/vendor/README.md](web/vendor/README.md) pour le reconstruire), chargé après le premier affichage. Sans WebGL 2, en mode économie de données ou avec « Réduire les animations », il n'y a ni 3D ni ouverture et l'appli reste entière.

## Hors connexion

L'appli s'ouvre même quand Musaarr est injoignable (réseau coupé, serveur éteint) : un service worker garde la dernière version de la page, la dernière liste des demandes reste lisible, et un bandeau dit quoi vérifier. Les fichiers de la page partent compressés et ne sont retéléchargés que s'ils ont changé.

## Configuration

Aucune variable n'est obligatoire. Au premier lancement, après la création du compte administrateur, la page **Réglages** (`/setup.html`) demande l'adresse et la clé de Lidarr, les teste, puis propose ses dossiers et profils dans des listes ; Plex se branche par un code à approuver sur plex.tv, qui donne la liste des serveurs du compte et l'adresse où chacun répond. Le jeton ListenBrainz et Prowlarr s'y ajoutent aussi. Enregistrer relance Musaarr de lui-même, sans redémarrer le conteneur. La page reste ensuite accessible aux administrateurs (lien « Réglages » dans l'en-tête).

Les réglages vont dans `DATA_DIR/settings.json` (droits 600, versionné). Une clé ou un jeton n'en ressort jamais : la page sait seulement qu'il y en a un. Une variable d'environnement l'emporte sur le fichier et fige le champ dans la page, pour qu'un déploiement automatisé garde la main. Une installation d'avant `settings.json` (configurée par variables) est recopiée dans le fichier au premier démarrage, avec ses anciens défauts (`Hi-Fi`, `/data/music`) : rien ne change pour elle.

| Variable | Défaut | Rôle |
|---|---|---|
| `LIDARR_URL` | `http://lidarr:8686` | Adresse de Lidarr |
| `LIDARR_API_KEY` | vide (demandée par l'assistant) | Clé d'API de Lidarr |
| `PLEX_URL` | vide (Plex désactivé) | Adresse du serveur Plex (l'assistant la remplit après le code plex.tv) |
| `PLEX_TOKEN` | obligatoire si `PLEX_URL` | Jeton du titulaire du serveur |
| `PROWLARR_URL` | `http://prowlarr:9696` | Adresse de Prowlarr, pour l'onglet Sources |
| `PROWLARR_API_KEY` | vide (sources désactivées) | Clé d'API de Prowlarr (Settings → General) |
| `LISTENBRAINZ_URL` | `https://api.listenbrainz.org` | Écoutes des albums ; `off` les coupe, ainsi que les incontournables |
| `LISTENBRAINZ_TOKEN` | vide (écoutes désactivées) | Jeton d'un compte ListenBrainz gratuit (Settings → User token) |
| `STOCK_URL` | la release `stock` du dépôt | Stock de départ du disquaire, relu chaque semaine ; `off` le coupe |
| `ALLOWED_LOGIN` | vide | Identité qu'un proxy envoie pour le compte administrateur, sans mot de passe (installation d'avant les comptes) ; met `AUTH_HEADER` à `Tailscale-User-Login` par défaut |
| `AUTH_HEADER` | vide (aucun) | En-tête d'identité d'un proxy de confiance, par ex. `Remote-User` (Authelia) ou `X-authentik-username` |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | Adresses (ou plages) d'où l'en-tête d'identité, `X-Forwarded-For` et `X-Forwarded-Proto` sont crus. Préférer l'adresse du seul proxy (`172.18.0.5`) à tout un réseau Docker : n'importe quel conteneur de la plage pourrait se faire passer pour n'importe qui |
| `DATA_DIR` | `/data` | Dossier de `settings.json`, `auth.json`, `requests.json`, `room.json`, `stock.json.gz`, `shop.json` et `obi.json` |
| `QUALITY_PROFILE` | choisi dans l'assistant | Profil de qualité des nouveaux artistes |
| `METADATA_PROFILE` | `None` | Profil de métadonnées des nouveaux artistes |
| `ROOT_FOLDER` | choisi dans l'assistant | Dossier racine Lidarr |
| `LISTEN_ADDR` | `127.0.0.1:8080` | Adresse d'écoute (`:8080` dans l'image publiée) |
| `METADATA_URL` | `https://api.lidarr.audio/api/v0.4` | Serveur de métadonnées de Lidarr, lu directement pour lister les albums d'un artiste |
| `HIRES_TAGS` | vide | Étiquettes qu'un indexeur met dans le titre d'une sortie 24 bits sans perte, séparées par des virgules |
| `MULTICHANNEL_TAGS` | vide | Étiquettes qui signalent un mixage multicanal, séparées par des virgules |

`DATA_DIR`, `LISTEN_ADDR`, `ALLOWED_LOGIN`, `AUTH_HEADER` et `TRUSTED_PROXIES` ne viennent que de l'environnement : une erreur dans l'un d'eux pourrait fermer la page qui sert à la corriger. Toutes les autres se règlent aussi dans la page.

### Connexion

- **Premier lancement** : sans aucun compte, la page de connexion propose de créer le compte administrateur. Ce n'est possible que depuis le réseau local (boucle locale, adresses privées, Tailscale) : un port publié sur Internet ne livre pas une installation neuve au premier venu. Attention, Docker relaie certaines connexions (IPv6, ou venues de l'hôte lui-même) en leur donnant l'adresse de sa passerelle, qui est privée : publier le port sur l'adresse du réseau local (`192.168.1.10:8080:8080`) plutôt que sur toutes, et créer le compte dès l'installation. Derrière un proxy, il doit être dans `TRUSTED_PROXIES` et donner l'adresse du visiteur dans `X-Forwarded-For` ; un proxy inconnu cache qui est derrière lui, et la création est refusée.
- **Mot de passe** : chaque compte a son nom et son mot de passe (8 caractères au moins), gardé haché (PBKDF2-SHA256, 600 000 tours) dans `auth.json` (droits 600). La session tient dans un cookie `HttpOnly` et `SameSite=Lax`, `Secure` en HTTPS ; elle prend fin après 30 jours sans visite. Dix essais ratés depuis une même adresse en un quart d'heure mettent les suivants en pause (derrière un proxy absent de `TRUSTED_PROXIES`, tout le monde partage son adresse, et donc cette pause). Un nom inconnu et un mauvais mot de passe reçoivent la même réponse.
- **Proxy de confiance** : avec `AUTH_HEADER`, la personne que le proxy nomme entre sans mot de passe, si un compte porte cette identité (aujourd'hui, celui d'`ALLOWED_LOGIN`) et si la requête vient d'une adresse de `TRUSTED_PROXIES`. Venu d'ailleurs, l'en-tête est ignoré, et les journaux le disent une fois par adresse. Un en-tête ne crée jamais de compte, et ne se confond jamais avec le nom d'un compte. Le proxy doit retirer les copies de l'en-tête envoyées par le navigateur (Tailscale serve le fait), et écrire ou compléter `X-Forwarded-For` plutôt que transmettre celui du navigateur.
- **Mot de passe oublié** : arrêter Musaarr, retirer `auth.json` de `DATA_DIR`, relancer, puis recréer le compte administrateur depuis le réseau local.

Tant que Lidarr n'est pas réglé, Musaarr n'ouvre que l'assistant : la page d'accueil y mène, l'API répond 503 (`setup.required`) et `/healthz` répond 200, car le conteneur va bien. Une fois réglé, Musaarr vérifie au démarrage que le profil de qualité, le profil de métadonnées et le dossier racine existent ; tant que ce n'est pas le cas, `/healthz` répond 503 (la raison est dans les journaux du conteneur) et les demandes sont refusées.

### Sauvegarde

La page des réglages télécharge un `.zip` de ce que Musaarr ne sait pas reconstruire : `auth.json`, `settings.json`, `requests.json` et `room.json`. Le stock du disquaire, ses pochettes et l'index Plex se refont seuls. Le fichier contient les clés et les mots de passe hachés : le garder comme un mot de passe. Pour changer de machine, lancer la nouvelle, créer le compte administrateur, puis restaurer le fichier depuis la même page.

La restauration lit et vérifie tout le fichier avant de remplacer quoi que ce soit, puis relance l'app. Elle refuse une archive sans administrateur, un fichier illisible et des réglages d'une version plus récente. Une clé fixée par une variable d'environnement garde son adresse. Si les comptes restaurés ne contiennent pas celui de la personne qui restaure, elle est déconnectée. L'administrateur d'`ALLOWED_LOGIN` garde son accès par le proxy.

## Installer

Il faut Docker, un Lidarr joignable et, pour la bibliothèque, un serveur Plex en 1.43.3.10861 ou plus récent (les versions antérieures ont des failles activement exploitées). L'image `ghcr.io/smile2578/musaarrr` existe pour amd64 et arm64.

1. Copier [deploy/compose.yml](deploy/compose.yml) dans un dossier, puis `docker compose up -d`. Si Lidarr tourne dans un autre projet compose, rejoindre son réseau ou taper son adresse dans l'assistant.
2. Depuis le réseau local, ouvrir `http://<adresse de la machine>:8080` : la première visite crée le compte administrateur, puis l'assistant demande Lidarr (Settings › General › API Key) et branche Plex par un code.
3. Dans Lidarr, si « Allowed Hosts » est renseigné (Settings › General), y ajouter le nom sous lequel Musaarr le joint (`lidarr`). Vérifier aussi que la connexion « Plex Media Server » existe (Settings › Connect) avec la mise à jour de la bibliothèque à l'import : c'est elle qui fait apparaître l'album dans Plex.
4. Sur un téléphone ou une tablette, ouvrir la même adresse, puis « Sur l'écran d'accueil ».

Les données vont dans le volume `musaarr-data`. Pour un dossier de l'hôte à la place, le donner à l'utilisateur de l'image (`chown 65532:65532`), ou à celui qu'indique `user:` dans le compose.

### Derrière Tailscale

[deploy/compose.tailscale.yml](deploy/compose.tailscale.yml) ne rend Musaarr joignable que sur le tailnet, en HTTPS, par Tailscale serve. Musaarr y écoute sur `127.0.0.1:8080`, que seul le conteneur Tailscale atteint.

1. Dans la console Tailscale, activer MagicDNS et les certificats HTTPS (DNS › HTTPS Certificates), puis créer une clé d'authentification à usage unique (Settings › Keys) et la mettre dans le `.env` du compose : `TS_AUTHKEY_MUSAARR=<clé>`.
2. Créer `ts/config/serve.json` avec le contenu de [deploy/serve.json](deploy/serve.json), puis `docker compose -f compose.tailscale.yml up -d`.
3. Une fois la machine apparue dans la console, désactiver l'expiration de sa clé, puis retirer `TS_AUTHKEY_MUSAARR` du `.env`.
4. Ouvrir `https://<nom de la machine>.<tailnet>.ts.net`. Pour entrer par son identité Tailscale sans mot de passe, ajouter `AUTH_HEADER=Tailscale-User-Login` et `ALLOWED_LOGIN=<identifiant Tailscale>` (voir « Connexion »).

### Construire l'image soi-même

Depuis une copie du dépôt : `docker build -t musaarr .`, puis `image: musaarr` et `pull_policy: never` dans le compose. Cette image-là écoute sur `127.0.0.1:8080` : ajouter `LISTEN_ADDR=:8080` pour la joindre par un port publié.

## Mettre à jour

`docker compose pull && docker compose up -d`. `:latest` suit toutes les versions, `:vX` les versions d'une même majeure, `:vX.Y` seulement ses correctifs. Pour revenir en arrière, épingler la version précédente (`ghcr.io/smile2578/musaarrr:vX.Y.Z`). Les réglages et les comptes restent dans le volume ; une sauvegarde depuis la page Réglages avant une mise à jour ne coûte rien.

## Développer

```sh
go test -race ./...
DATA_DIR=./data go run .
node --test test/shop3d/*.test.mjs
node --test test/e2e/*.test.mjs   # le premier lancement dans Chromium (Playwright), contre un faux Lidarr et un faux Plex
```

En local, la première visite de `http://127.0.0.1:8080` crée le compte administrateur, puis ouvre l'assistant. Avec `ALLOWED_LOGIN`, envoyer plutôt l'en-tête `Tailscale-User-Login: moi@exemple.fr` (extension de navigateur ou `curl -H`). `E2E_SHOTS=<dossier>` garde une capture de chaque étape du test navigateur.

### Versions et image

Les versions suivent les messages de commit conventionnels (`feat:`, `fix:`…). release-please garde ouverte sur `main` une PR de version, avec son CHANGELOG. La fusionner crée le tag `vX.Y.Z` et la release. GoReleaser ([.goreleaser.yaml](.goreleaser.yaml)) y joint ensuite les binaires Linux et publie l'image `ghcr.io/smile2578/musaarrr` pour amd64 et arm64, avec son SBOM. Tout est signé par cosign, sans clé, avec l'identité du workflow de release. Pour vérifier une image :

```sh
cosign verify ghcr.io/smile2578/musaarrr:vX.Y.Z \
  --certificate-identity https://github.com/Smile2578/musaarrr/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

L'image publiée ([Dockerfile.release](Dockerfile.release)) écoute sur `:8080`, et son `/data` appartient à l'utilisateur sans droits de l'image : un volume Docker neuf y est donc accessible en écriture. Le [Dockerfile](Dockerfile), lui, construit l'image depuis les sources sur la machine même, et garde `127.0.0.1:8080` pour un proxy qui partage son réseau.

Pour que release-please puisse ouvrir sa PR, le dépôt doit autoriser les Actions à créer des pull requests (Settings › Actions › General). Une image publiée pour la première fois sur GHCR est privée : la rendre publique se fait dans les réglages du paquet, sur GitHub.

## Licence

Musaarr est un logiciel libre, sous licence GNU GPL version 3 ou ultérieure ([LICENSE](LICENSE)).

Police : Libre Franklin, licence SIL Open Font License 1.1 ([web/fonts/LICENCE-polices.txt](web/fonts/LICENCE-polices.txt)). 3D : three.js, licence MIT ([web/vendor/LICENSE](web/vendor/LICENSE)).
