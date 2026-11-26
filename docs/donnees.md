# Les données du disquaire

Le disquaire de Musaarr part d'un **stock de départ** construit une fois par mois par la tâche `.github/workflows/stock.yml`, puis publié comme `stock.json.gz` dans la release `stock` du dépôt. Chaque installation le télécharge et y ajoute sa **couche personnelle**.

## Le stock de départ : CC0

Le fichier ne contient que des données placées dans le domaine public (CC0 1.0) par leurs auteurs.

| Donnée | Source | Licence |
|---|---|---|
| Identifiant, titre, crédit d'artiste, nom de classement, date de sortie, liens vers Wikidata et Discogs | [MusicBrainz](https://musicbrainz.org/doc/About/Data_License), dump JSON `release-group` | CC0 (données de base) |
| Rayon et style : genres de l'album (`P136`), sinon de ses interprètes (`P175`) | [Wikidata](https://www.wikidata.org/wiki/Wikidata:Licensing), Wikidata Query Service | CC0 |
| Ordre du stock (les plus écoutés d'abord) et choix des albums | [ListenBrainz](https://listenbrainz.org/), `POST /1/popularity/release-group` | licence des chiffres non confirmée : le fichier garde l'ordre, jamais les chiffres |
| Présence d'une pochette | ListenBrainz (`/1/metadata/release_group/`), d'après le [Cover Art Archive](https://coverartarchive.org/) | aucune image n'est livrée |

Le constructeur sait aussi lire le dump mensuel des masters de [Discogs](https://data.discogs.com/) (CC0, `-discogs`), qui range plus finement, mais data.discogs.com refuse les serveurs de GitHub : la tâche mensuelle s'en passe.

Le dump JSON de MusicBrainz contient aussi des genres, des étiquettes, des notes et des annotations, qui sont sous **CC BY-NC-SA 3.0**. Le constructeur ne les lit pas : aucun d'eux n'entre dans le fichier, qui reste librement réutilisable, y compris commercialement.

Les pochettes appartiennent à leurs ayants droit. Elles ne sont jamais livrées : chaque installation les lit à la demande auprès du Cover Art Archive et les garde dans son dossier de données.

## La couche personnelle : propre à chaque installation

Avec un jeton ListenBrainz, chaque installation ajoute « Ce mois-ci » (le classement global du mois) et les albums des artistes de sa Plex. Ces albums sont rangés d'après les étiquettes de MusicBrainz que renvoie ListenBrainz, sous **CC BY-NC-SA 3.0** : elles restent sur l'installation et ne sont jamais redistribuées.

## Soutenir MetaBrainz

MusicBrainz, ListenBrainz et le Cover Art Archive sont tenus par la fondation à but non lucratif MetaBrainz, qui demande un soutien même pour l'usage CC0 ([metabrainz.org/supporters](https://metabrainz.org/supporters)).
