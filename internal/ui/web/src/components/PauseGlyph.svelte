<script lang="ts">
  // The two bars that mean paused, in one place: the sites list draws them
  // beside a paused site and so does its tile, and the two must not drift into
  // saying different things about the same site.
  interface Props {
    size?: string;
    class?: string;
  }
  let { size = 'w-3 h-3', class: extra = '' }: Props = $props();
</script>

<svg class="{size} shrink-0 {extra}" fill="currentColor" viewBox="0 0 24 24" data-paused-glyph aria-hidden="true">
  <path d="M6 5h4v14H6zM14 5h4v14h-4z" />
</svg>
