cd /intelligences/apps/goph.re
bin/gophre update


gigit "Hourly update"

# Repack quand les objets loose s'accumulent (seuil gc.auto=200 dans .git/config) :
# sans ça, chaque version de rss_posts.json reste une copie complète non deltifiée.
git gc --auto --quiet
