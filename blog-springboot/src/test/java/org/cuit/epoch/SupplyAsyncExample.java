package org.cuit.epoch;

import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/18 18:10
 * @description: {}
 */
public class SupplyAsyncExample {

    public static void main(String[] args) throws ExecutionException, InterruptedException {
        // 查询推荐文章
        CompletableFuture<String> recommendArticleList = CompletableFuture.supplyAsync(() ->
                {
                    for (long i = 0; i < 1000000L; i++) {

                    }
                    return "supplyAsync 运行完成！";
                }

        );


        System.out.println("main 已经运行完成了！");
        System.out.println("recommendArticleList.get() = " + recommendArticleList.get());

    }
}
