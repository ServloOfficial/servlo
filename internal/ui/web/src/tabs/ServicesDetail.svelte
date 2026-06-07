<script lang="ts">
  import { routeRest } from '$stores/route';
  import { services } from '$stores/services';
  import ServiceDetail from './services/ServiceDetail.svelte';
  import ServicesDashboard from './services/ServicesDashboard.svelte';

  const selected = $derived($routeRest);
  const svc = $derived($services.find((s) => s.name === selected));
</script>

{#if svc}
  <ServiceDetail {svc} />
{:else}
  <ServicesDashboard />
{/if}
