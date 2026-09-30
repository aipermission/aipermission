export type ObservationScheduler = (_observe: () => Promise<void>, _delay: number) => () => void;

export const scheduleObservation: ObservationScheduler = (observe, delay) => {
  const timer = setTimeout(() => {
    void observe();
  }, delay);
  return () => clearTimeout(timer);
};
