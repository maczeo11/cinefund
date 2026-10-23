// The product in three steps, in plain words. Shown in full on the home page
// and as a compact list beside the pledge form, so a first-time visitor always
// knows what to do next.

const STEPS = [
  {
    title: 'Find a film',
    body: 'Browse campaigns by genre. Each film has a page with its story, rewards and a reel you can stream.',
  },
  {
    title: 'Back it',
    body: 'Choose a reward (or any amount you like) and pay securely with Razorpay. You are only counted once your payment is confirmed.',
  },
  {
    title: 'Watch & follow',
    body: 'Sign in to stream the filmmaker’s reel on the film’s page. If a film misses its goal by the deadline, every backer is refunded in full.',
  },
] as const

export default function HowItWorks({ compact = false }: { compact?: boolean }) {
  if (compact) {
    return (
      <ol className="space-y-2.5" aria-label="How backing works">
        {STEPS.map((s, i) => (
          <li key={s.title} className="flex gap-3 text-xs">
            <span
              className="mt-px h-5 w-5 shrink-0 rounded-full border border-amber/40 bg-amber/10 text-amber font-mono font-bold flex items-center justify-center text-[10px]"
              aria-hidden="true"
            >
              {i + 1}
            </span>
            <span className="text-silver-dim leading-relaxed">
              <span className="text-silver font-semibold">{s.title}.</span> {s.body}
            </span>
          </li>
        ))}
      </ol>
    )
  }

  return (
    <section aria-labelledby="how-heading" className="py-12 border-t border-white/[0.08]">
      <h2 id="how-heading" className="font-cinema text-2xl sm:text-3xl font-bold text-silver tracking-wide">
        How CineFund works
      </h2>
      <ol className="mt-8 grid gap-4 md:grid-cols-3">
        {STEPS.map((s, i) => (
          <li key={s.title} className="rounded-2xl border border-white/[0.08] bg-celluloid p-6">
            <span
              className="h-9 w-9 rounded-full bg-amber/15 border border-amber/30 text-amber font-cinema font-bold flex items-center justify-center"
              aria-hidden="true"
            >
              {i + 1}
            </span>
            <h3 className="mt-4 font-cinema text-lg font-bold text-silver">{s.title}</h3>
            <p className="mt-2 text-sm text-silver-dim leading-relaxed">{s.body}</p>
          </li>
        ))}
      </ol>
    </section>
  )
}
