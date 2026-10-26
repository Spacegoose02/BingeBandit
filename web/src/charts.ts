/** Charts a collection can follow, by the link of the chart's own page (see the server's importer/charts.go). */
export interface Chart {
  name: string
  url: string
  kind: 'movies' | 'shows'
  source: 'TMDB' | 'Trakt' | 'IMDb'
}

export const charts: Chart[] = [
  { source: 'TMDB', kind: 'movies', name: 'Trending Movies (today)', url: 'https://www.themoviedb.org/trending/movie/day' },
  { source: 'TMDB', kind: 'movies', name: 'Trending Movies (this week)', url: 'https://www.themoviedb.org/trending/movie/week' },
  { source: 'TMDB', kind: 'movies', name: 'Popular Movies', url: 'https://www.themoviedb.org/movie' },
  { source: 'TMDB', kind: 'movies', name: 'Top Rated Movies', url: 'https://www.themoviedb.org/movie/top-rated' },
  { source: 'TMDB', kind: 'movies', name: 'Now Playing', url: 'https://www.themoviedb.org/movie/now-playing' },
  { source: 'TMDB', kind: 'movies', name: 'Upcoming Movies', url: 'https://www.themoviedb.org/movie/upcoming' },
  { source: 'TMDB', kind: 'shows', name: 'Trending TV (today)', url: 'https://www.themoviedb.org/trending/tv/day' },
  { source: 'TMDB', kind: 'shows', name: 'Trending TV (this week)', url: 'https://www.themoviedb.org/trending/tv/week' },
  { source: 'TMDB', kind: 'shows', name: 'Popular TV', url: 'https://www.themoviedb.org/tv' },
  { source: 'TMDB', kind: 'shows', name: 'Top Rated TV', url: 'https://www.themoviedb.org/tv/top-rated' },
  { source: 'TMDB', kind: 'shows', name: 'On the Air', url: 'https://www.themoviedb.org/tv/on-the-air' },
  { source: 'Trakt', kind: 'movies', name: 'Trending Movies', url: 'https://trakt.tv/movies/trending' },
  { source: 'Trakt', kind: 'movies', name: 'Popular Movies', url: 'https://trakt.tv/movies/popular' },
  { source: 'Trakt', kind: 'movies', name: 'Anticipated Movies', url: 'https://trakt.tv/movies/anticipated' },
  { source: 'Trakt', kind: 'movies', name: 'Box Office', url: 'https://trakt.tv/movies/boxoffice' },
  { source: 'Trakt', kind: 'movies', name: 'Most Watched Movies This Week', url: 'https://trakt.tv/movies/watched/weekly' },
  { source: 'Trakt', kind: 'shows', name: 'Trending Shows', url: 'https://trakt.tv/shows/trending' },
  { source: 'Trakt', kind: 'shows', name: 'Popular Shows', url: 'https://trakt.tv/shows/popular' },
  { source: 'Trakt', kind: 'shows', name: 'Anticipated Shows', url: 'https://trakt.tv/shows/anticipated' },
  { source: 'Trakt', kind: 'shows', name: 'Most Watched Shows This Week', url: 'https://trakt.tv/shows/watched/weekly' },
  { source: 'IMDb', kind: 'movies', name: 'IMDb Top 250', url: 'https://www.imdb.com/chart/top/' },
  { source: 'IMDb', kind: 'movies', name: 'IMDb Most Popular Movies', url: 'https://www.imdb.com/chart/moviemeter/' },
  { source: 'IMDb', kind: 'shows', name: 'IMDb Top 250 TV', url: 'https://www.imdb.com/chart/toptv/' },
  { source: 'IMDb', kind: 'shows', name: 'IMDb Most Popular TV', url: 'https://www.imdb.com/chart/tvmeter/' },
]

/** The charts that fit a library ("movies" or "shows"). */
export const chartsFor = (kind: string) => charts.filter((c) => c.kind === (kind === 'shows' ? 'shows' : 'movies'))
