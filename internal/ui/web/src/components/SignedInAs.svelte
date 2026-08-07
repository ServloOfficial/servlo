<script lang="ts">
  import { session, signOut } from '$stores/session';

  // Who is signed in, and the way out. Both in the header rather than a
  // settings tab: on a shared server "which account am I using" is a question
  // worth being able to answer without navigating anywhere.
</script>

{#if $session.authenticated}
  <div class="flex items-center gap-2 text-xs" data-signed-in-as>
    <span class="text-gray-500 dark:text-gray-400">
      {$session.user}{#if $session.role === 'developer'}<span class="ml-1 text-gray-400 dark:text-gray-500">(developer)</span>{/if}
    </span>
    <button
      onclick={() => signOut()}
      disabled={$session.busy}
      class="rounded-md px-2 py-1 font-medium text-gray-600 hover:text-gray-900 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-100 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
    >
      Sign out
    </button>
  </div>
{/if}
