package org.cuit.epoch.controller;

import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.annotation.OptLog;
import org.cuit.epoch.dto.category.CategoryBackDTO;
import org.cuit.epoch.dto.category.CategoryDTO;
import org.cuit.epoch.dto.category.CategoryOptionDTO;
import org.cuit.epoch.service.CategoryService;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.CategoryVO;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.validation.Valid;
import java.util.List;

import static org.cuit.epoch.constant.OptTypeConst.REMOVE;
import static org.cuit.epoch.constant.OptTypeConst.SAVE_OR_UPDATE;

/**
 * @author: ladidol
 * @date: 2022/12/2 20:11
 * @description:
 */
@Api(tags = "分类模块")
@RestController
public class CategoryController {
    @Autowired
    private CategoryService categoryService;

    /**
     * 查看分类列表
     *
     * @return {@link Result < CategoryDTO >} 分类列表
     */
    @ApiOperation(value = "查看分类列表")
    @GetMapping("/categories")
    public Result<PageResult<CategoryDTO>> listCategories() {
        return Result.ok(categoryService.listCategories());
    }

    /**
     * 查看后台分类列表
     *
     * @param condition 条件
     * @return {@link Result< CategoryBackDTO >} 后台分类列表
     */
    @ApiOperation(value = "查看后台分类列表")
    @GetMapping("/admin/categories")
    public Result<PageResult<CategoryBackDTO>> listBackCategories(ConditionVO condition) {
        return Result.ok(categoryService.listBackCategories(condition));
    }

    /**
     * 搜索文章分类
     *
     * @param condition 条件
     * @return {@link Result< CategoryOptionDTO >} 分类列表
     */
    @ApiOperation(value = "模糊搜索分类")
    @GetMapping("/admin/categories/search")
    public Result<List<CategoryOptionDTO>> listCategoriesBySearch(ConditionVO condition) {
        return Result.ok(categoryService.listCategoriesBySearch(condition));
    }

    /**
     * 添加或修改分类
     *
     * @param categoryVO 分类信息
     * @return {@link Result<>}
     */
    @OptLog(optType = SAVE_OR_UPDATE)
    @ApiOperation(value = "添加或修改分类")
    @PostMapping("/admin/categories")
    public Result<?> saveOrUpdateCategory(@Valid @RequestBody CategoryVO categoryVO) {
        categoryService.saveOrUpdateCategory(categoryVO);
        return Result.ok();
    }

    /**
     * 删除分类
     *
     * @param categoryIdList 分类id列表
     * @return {@link Result<>}
     */
    @OptLog(optType = REMOVE)
    @ApiOperation(value = "删除分类")
    @DeleteMapping("/admin/categories")
    public Result<?> deleteCategories(@RequestBody List<Integer> categoryIdList) {
        categoryService.deleteCategory(categoryIdList);
        return Result.ok();
    }

}