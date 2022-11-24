package org.cuit.epoch;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * @author: ladidol
 * @date: 2022/11/24 15:32
 * @description: 一个在RocketMQ中出现的一个小东西，感觉很棒。
 */

public class ThreadSleep {

    public static AtomicInteger num = new AtomicInteger(0);


    /*非正常情况！！！！！！！！！！*/
    //int i = 0的循环会出现这个问题，先执行完子线程再结束主线程
    //Thread-0执行结束!
    //Thread-1执行结束!
    //num=2000000000

    /*正常情况！！！！！！！！！！*/
    //long i = 0的循环则不会出现这个问题，只是先结束主线程，直接可以拿到Safepoint安全点
    //num=121208030
    //Thread-0执行结束!
    //Thread-1执行结束!

    //加一个Thread.sleep(0)操作也可以直接结束主线程
    //num=123051687
    //Thread-0执行结束!
    //Thread-1执行结束!



    public static void main(String[] args) throws InterruptedException {
        Runnable runnable = () -> {
            for (int i = 0; i < 1000000000; i++) {
//            for (long i = 0; i < 1000000000; i++) {
                num.getAndAdd(1);

                if (i%1000==0){
                    try {
                        Thread.sleep(0);
                    } catch (InterruptedException e) {
                        throw new RuntimeException(e);
                    }
                }

            }
            System.out.println(Thread.currentThread().getName() + "执行结束!");
        };

        Thread t1 = new Thread(runnable);
        Thread t2 = new Thread(runnable);
        t1.start();
        t2.start();
        Thread.sleep(1000);
        System.out.println("num=" + num);
    }
}